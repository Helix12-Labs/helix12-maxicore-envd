package webdev

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"connectrpc.com/connect"

	runtimev1 "github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/services/runtime/v1"
)

// SaveCheckpoint creates a tar.gz of the project state and uploads it to
// the provided S3 presigned URL. Implements WebdevService.SaveCheckpoint.
//
// Manus 1:1 flow:
//
//  1. lock project (no parallel save/restore on same project)
//  2. tar.gz project dir (excluding node_modules, .git, __pycache__, .manus-logs)
//  3. PUT to checkpoint_zip_upload_url (S3 presigned)
//  4. record current git-sha as last_checkpoint_commit
//  5. return JSON-encoded result in SaveCheckpointResponse.data
//
// Errors are wrapped with connect.CodeInvalidArgument/CodeInternal appropriately.
func (s *Service) SaveCheckpoint(
	ctx context.Context,
	req *connect.Request[runtimev1.SaveCheckpointRequest],
) (*connect.Response[runtimev1.SaveCheckpointResponse], error) {
	in := req.Msg
	if in.GetProjectName() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("project_name is required"))
	}
	if in.GetCheckpointZipUploadUrl() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("checkpoint_zip_upload_url is required"))
	}

	ctx, cancel := withCheckpointTimeout(ctx)
	defer cancel()

	unlock := s.lockProject(in.GetProjectName())
	defer unlock()

	projectPath, err := s.projectPath(in.GetProjectName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	exists, err := s.projectExists(in.GetProjectName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if !exists {
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("%w: %s", ErrProjectNotFound, in.GetProjectName()))
	}

	// 1. tar.gz the project directory
	zipPath, zipSize, err := s.createCheckpointZip(ctx, projectPath, in.GetProjectName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("createCheckpointZip: %w", err))
	}
	defer os.Remove(zipPath)

	// 2. upload via PUT to S3 presigned URL
	if err := uploadToPresignedURL(ctx, zipPath, in.GetCheckpointZipUploadUrl()); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("uploadToPresignedURL: %w", err))
	}

	// 3. record git-sha (best-effort; not all projects are git-tracked)
	commit, _ := currentGitCommit(ctx, projectPath)

	// 4. assemble response payload (Manus encodes result as JSON in `data` field)
	result := map[string]any{
		"status":                 "ok",
		"project_name":           in.GetProjectName(),
		"checkpoint_zip_url":     in.GetCheckpointZipUrl(), // echo back if caller provided GET URL
		"last_checkpoint_commit": commit,
		"size_bytes":             zipSize,
		"timestamp":              time.Now().UTC().Format(time.RFC3339),
	}
	if in.GetDescription() != "" {
		result["description"] = in.GetDescription()
	}
	data, _ := json.Marshal(result)

	s.logger.Info().
		Str("project", in.GetProjectName()).
		Str("commit", commit).
		Int64("size_bytes", zipSize).
		Msg("SaveCheckpoint completed")

	return connect.NewResponse(&runtimev1.SaveCheckpointResponse{Data: string(data)}), nil
}

// excludePatterns lists path-fragments to skip during checkpoint tar.gz creation.
// Verified against Manus rclone exclude-set.
var excludePatterns = []string{
	"node_modules",
	".git",
	"__pycache__",
	".manus-logs",
	".maxicore-logs",
	".next",
	"dist",
	"build",
	".cache",
	".pnpm-store",
	".venv",
	"venv",
	"target", // rust build artifact
}

// shouldExclude returns true if rel-path begins with or contains any exclude-pattern segment.
func shouldExclude(relPath string) bool {
	if relPath == "" || relPath == "." {
		return false
	}
	parts := strings.Split(filepath.ToSlash(relPath), "/")
	for _, part := range parts {
		for _, ex := range excludePatterns {
			if part == ex {
				return true
			}
		}
	}
	return false
}

// createCheckpointZip creates a tar.gz of projectPath at a temp location.
// Returns (zip-path, size, error). The caller is responsible for os.Remove(zipPath).
func (s *Service) createCheckpointZip(ctx context.Context, projectPath, projectName string) (string, int64, error) {
	tmpFile, err := os.CreateTemp("", "checkpoint-"+sanitize(projectName)+"-*.tar.gz")
	if err != nil {
		return "", 0, err
	}
	zipPath := tmpFile.Name()

	gzw := gzip.NewWriter(tmpFile)
	tw := tar.NewWriter(gzw)

	closeAll := func() error {
		var firstErr error
		if err := tw.Close(); err != nil {
			firstErr = err
		}
		if err := gzw.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		if err := tmpFile.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		return firstErr
	}

	err = filepath.Walk(projectPath, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(projectPath, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if shouldExclude(rel) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = rel

		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			hdr.Linkname = target
		}

		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}

		if info.Mode().IsRegular() {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			if _, err := io.Copy(tw, f); err != nil {
				_ = f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		}
		return nil
	})

	if cerr := closeAll(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(zipPath)
		return "", 0, err
	}

	stat, err := os.Stat(zipPath)
	if err != nil {
		_ = os.Remove(zipPath)
		return "", 0, err
	}
	return zipPath, stat.Size(), nil
}

// uploadToPresignedURL performs an HTTP PUT of the file at zipPath to presignedURL.
// Content-Length is set from the file size; Content-Type is application/gzip.
func uploadToPresignedURL(ctx context.Context, zipPath, presignedURL string) error {
	if presignedURL == "" {
		return errors.New("empty presigned URL")
	}
	f, err := os.Open(zipPath)
	if err != nil {
		return err
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, presignedURL, f)
	if err != nil {
		return err
	}
	req.ContentLength = stat.Size()
	req.Header.Set("Content-Type", "application/gzip")

	client := &http.Client{Timeout: DefaultCheckpointTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("PUT failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024))
		return fmt.Errorf("S3 PUT %s: %d %s", presignedURL, resp.StatusCode, string(body))
	}
	return nil
}

// currentGitCommit returns the current HEAD SHA of the git repo at projectPath.
// If projectPath is not a git repo or git is unavailable, returns "" (no error).
// Used for last_checkpoint_commit anchoring.
func currentGitCommit(ctx context.Context, projectPath string) (string, error) {
	gitDir := filepath.Join(projectPath, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		return "", nil // not a git repo
	}
	gitCtx, cancel := context.WithTimeout(ctx, DefaultGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(gitCtx, "git", "-C", projectPath, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return "", nil // git failed; non-fatal for checkpoint
	}
	return strings.TrimSpace(string(out)), nil
}

// sanitize returns a filesystem-safe version of name for use in temp paths.
func sanitize(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}
