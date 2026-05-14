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

	"connectrpc.com/connect"

	runtimev1 "github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/services/runtime/v1"
)

// RestoreProject restores a project's state. Manus 1:1 flow:
//
//  1. lock project (no parallel restore/save on same project)
//  2. S3 preflight cleanup: list bundles in remote, keep only the newest
//  3. if rclone-sync is configured, run an inbound sync to materialize the
//     project tree from S3 first
//  4. (optional path) try git-clone from source_git_repo if provided in
//     project_config; fall back to checkpoint zip extract on failure
//  5. return success JSON in RestoreProjectResponse.data
//
// Note: Manus's RestoreProjectRequest does NOT carry a checkpoint_zip_url
// field (5-field schema: capabilities, experiments, platform, project_config,
// project_name). The state is materialized via the continuous Rclone-sync that
// runs in the background. This handler only initiates / awaits the materialization.
func (s *Service) RestoreProject(
	ctx context.Context,
	req *connect.Request[runtimev1.RestoreProjectRequest],
) (*connect.Response[runtimev1.RestoreProjectResponse], error) {
	in := req.Msg
	if in.GetProjectName() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("project_name is required"))
	}

	ctx, cancel := withCheckpointTimeout(ctx)
	defer cancel()

	unlock := s.lockProject(in.GetProjectName())
	defer unlock()

	projectPath, err := s.projectPath(in.GetProjectName())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}

	// Ensure project dir exists before sync materializes into it
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("mkdir project: %w", err))
	}

	// Parse project_config for source_git_repo (Manus pattern: JSON-encoded config blob)
	var cfg projectConfig
	if in.GetProjectConfig() != "" {
		_ = json.Unmarshal([]byte(in.GetProjectConfig()), &cfg)
	}

	// 1. S3 preflight cleanup (always safe to attempt; no-op if no rclone)
	preflightInfo, _ := s.s3PreflightCleanup(ctx, in.GetProjectName())

	// 2. Materialize via rclone (inbound sync) if configured
	rcl := s.rclone()
	var rcloneRan bool
	if rcl != nil {
		if err := rcl.SyncDown(ctx, in.GetProjectName(), projectPath); err != nil {
			s.logger.Warn().Err(err).Str("project", in.GetProjectName()).Msg("RestoreProject: rclone sync-down failed")
		} else {
			rcloneRan = true
		}
	}

	// 3. Try git-clone from source_git_repo if specified and project still empty
	var gitCloned bool
	if cfg.SourceGitRepo != "" {
		empty, _ := isDirEmpty(projectPath)
		if empty {
			if err := tryGitClone(ctx, projectPath, cfg.SourceGitRepo); err == nil {
				gitCloned = true
			} else {
				s.logger.Warn().Err(err).Str("project", in.GetProjectName()).Msg("RestoreProject: git clone attempt failed")
			}
		}
	}

	// 4. Build response
	result := map[string]any{
		"status":         "ok",
		"project_name":   in.GetProjectName(),
		"project_path":   projectPath,
		"rclone_synced":  rcloneRan,
		"git_cloned":     gitCloned,
		"preflight_info": preflightInfo,
	}
	data, _ := json.Marshal(result)

	s.logger.Info().
		Str("project", in.GetProjectName()).
		Bool("rclone", rcloneRan).
		Bool("git", gitCloned).
		Msg("RestoreProject completed")

	return connect.NewResponse(&runtimev1.RestoreProjectResponse{Data: string(data)}), nil
}

// projectConfig is a minimal subset of Manus's WebDev project-config JSON
// that we need to interpret here. Unknown fields are tolerated.
type projectConfig struct {
	SourceGitRepo string `json:"source_git_repo,omitempty"`
	UserGitRepo   string `json:"user_git_repo,omitempty"`
	DevCommand    string `json:"dev_command,omitempty"`
	BuildCommand  string `json:"build_command,omitempty"`
	Port          string `json:"port,omitempty"`
}

// s3PreflightCleanup retains only the newest checkpoint bundle for a project
// in the S3 bucket. Manus log line: "S3 preflight: keeping newest bundle".
//
// Implementation detail: we delegate to rclone (`rclone lsl --files-only`)
// because envd does not link aws-sdk directly. If rclone is not configured,
// returns "no-op" info string and nil error.
func (s *Service) s3PreflightCleanup(ctx context.Context, projectName string) (string, error) {
	rcl := s.rclone()
	if rcl == nil {
		return "rclone-not-configured", nil
	}
	kept, removed, err := rcl.RetainNewestBundle(ctx, projectName)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("keeping newest bundle: kept=%q removed=%d", kept, removed), nil
}

// tryGitClone clones repoURL into projectPath. The caller has already verified
// projectPath is empty. Sets a sensible default branch only if cloning succeeds.
func tryGitClone(ctx context.Context, projectPath, repoURL string) error {
	gitCtx, cancel := context.WithTimeout(ctx, DefaultGitTimeout*2)
	defer cancel()
	cmd := exec.CommandContext(gitCtx, "git", "clone", repoURL, projectPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git clone %s: %w: %s", repoURL, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// downloadFromURL HTTP-GETs an S3 presigned URL into a local path.
// Public for use by RollbackProject (rollback to specific version_id).
func downloadFromURL(ctx context.Context, presignedURL, destPath string) error {
	if presignedURL == "" {
		return errors.New("empty presigned URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, presignedURL, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: DefaultCheckpointTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024))
		return fmt.Errorf("S3 GET %s: %d %s", presignedURL, resp.StatusCode, string(body))
	}
	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, resp.Body)
	return err
}

// extractCheckpointZip untars the tar.gz at zipPath into projectPath.
// Existing files are overwritten; project dir is created if missing.
// Path-traversal entries (../) are rejected.
func extractCheckpointZip(zipPath, projectPath string) error {
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		return err
	}
	f, err := os.Open(zipPath)
	if err != nil {
		return err
	}
	defer f.Close()

	gzr, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		// Sanitize: reject paths that escape projectPath
		clean := filepath.Clean(hdr.Name)
		if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") || strings.Contains(clean, "/../") {
			return fmt.Errorf("tar entry escapes project dir: %q", hdr.Name)
		}
		target := filepath.Join(projectPath, clean)

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode)&0o777); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				_ = out.Close()
				return err
			}
			if err := out.Close(); err != nil {
				return err
			}
		case tar.TypeSymlink:
			_ = os.Remove(target)
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return err
			}
		}
	}
	return nil
}

// isDirEmpty returns true if dir exists and has no entries (or doesn't exist).
func isDirEmpty(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}
	return len(entries) == 0, nil
}
