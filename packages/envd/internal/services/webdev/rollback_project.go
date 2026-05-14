package webdev

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"connectrpc.com/connect"

	runtimev1 "github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/services/runtime/v1"
)

// RollbackProject reverts a project to a previous version. Two paths:
//
//  1. If version_id is a git commit SHA → `git reset --hard <sha>`
//  2. Else if checkpoint_zip_url is provided → download + extract over current state
//
// Manus 1:1 fields:
//
//	checkpoint_zip_upload_url, checkpoint_zip_url, project_config,
//	project_name, version_id
func (s *Service) RollbackProject(
	ctx context.Context,
	req *connect.Request[runtimev1.RollbackProjectRequest],
) (*connect.Response[runtimev1.RollbackProjectResponse], error) {
	in := req.Msg
	if in.GetProjectName() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("project_name is required"))
	}
	if in.GetVersionId() == "" && in.GetCheckpointZipUrl() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("version_id or checkpoint_zip_url is required"))
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

	var method, anchor string

	// Path 1: git reset to version_id if it looks like a SHA and repo is git-tracked
	if isLikelyGitSHA(in.GetVersionId()) {
		if err := gitResetHard(ctx, projectPath, in.GetVersionId()); err == nil {
			method = "git-reset"
			anchor = in.GetVersionId()
		} else {
			s.logger.Warn().Err(err).Str("project", in.GetProjectName()).Msg("Rollback: git reset failed, trying zip")
		}
	}

	// Path 2: zip download + extract (overwrite)
	if method == "" && in.GetCheckpointZipUrl() != "" {
		tmp, err := os.CreateTemp("", "rollback-"+sanitize(in.GetProjectName())+"-*.tar.gz")
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		zipPath := tmp.Name()
		_ = tmp.Close()
		defer os.Remove(zipPath)

		if err := downloadFromURL(ctx, in.GetCheckpointZipUrl(), zipPath); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("download zip: %w", err))
		}

		// Clean the project dir of build artifacts before extract (preserve .git)
		if err := cleanProjectExceptGit(projectPath); err != nil {
			s.logger.Warn().Err(err).Msg("Rollback: cleanProjectExceptGit failed (continuing)")
		}

		if err := extractCheckpointZip(zipPath, projectPath); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("extract zip: %w", err))
		}
		method = "zip-extract"
		anchor = in.GetCheckpointZipUrl()
	}

	if method == "" {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("no usable rollback path (no SHA match, no zip url)"))
	}

	result := map[string]any{
		"status":       "ok",
		"project_name": in.GetProjectName(),
		"method":       method,
		"anchor":       anchor,
	}
	data, _ := json.Marshal(result)

	s.logger.Info().
		Str("project", in.GetProjectName()).
		Str("method", method).
		Msg("RollbackProject completed")

	return connect.NewResponse(&runtimev1.RollbackProjectResponse{Data: string(data)}), nil
}

// isLikelyGitSHA returns true if s looks like a 7-40 char hex string.
func isLikelyGitSHA(s string) bool {
	if len(s) < 7 || len(s) > 40 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9',
			r >= 'a' && r <= 'f',
			r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

// gitResetHard runs `git reset --hard <sha>` at projectPath.
// Returns an error if the dir is not a git repo or sha is unknown.
func gitResetHard(ctx context.Context, projectPath, sha string) error {
	if _, err := os.Stat(filepath.Join(projectPath, ".git")); err != nil {
		return errors.New("not a git repository")
	}
	gitCtx, cancel := context.WithTimeout(ctx, DefaultGitTimeout)
	defer cancel()
	cmd := exec.CommandContext(gitCtx, "git", "-C", projectPath, "reset", "--hard", sha)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git reset --hard %s: %w: %s", sha, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// cleanProjectExceptGit removes everything in projectPath except .git.
// Used before extract-overwrite to avoid stale files.
func cleanProjectExceptGit(projectPath string) error {
	entries, err := os.ReadDir(projectPath)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Name() == ".git" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(projectPath, e.Name())); err != nil {
			return err
		}
	}
	return nil
}
