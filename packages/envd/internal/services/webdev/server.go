// Package webdev implements the Manus 1:1 WebdevService Connect-RPC service.
//
// Service: runtime.v1.WebdevService (14 RPCs)
//
// This package provides the in-VM implementation that orchestrates web-project
// lifecycle: init, config, build, restart, restore, rollback, checkpoint, edits.
//
// Reverse-engineered from manus-sandbox v8.0.2:
//
//	github.com/manus-ai/manus-sandbox/sbx-go-svc/pkg/runtime/tools/webdev/
//
// Implementation notes:
//
//   - Checkpoint flow uses S3 presigned URLs (PUT for upload, GET for restore).
//     Backend generates URLs; envd only HTTP-PUT/GET against them.
//
//   - Continuous S3-sync runs via rclone subprocess (Manus uses rclone, NOT
//     aws-sdk-go directly). Configured at runtime via RefreshS3Token RPC.
//
//   - Project storage: /opt/maxicore-fc/webdev/projects/{project_name}/
//
//   - Git is used as source-of-truth: tryGitClone before unzip during restore,
//     git rev-parse HEAD as last_checkpoint_commit anchor.
package webdev

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

const (
	// DefaultProjectsBase is the on-disk root for all webdev projects.
	// Matches Manus's WEBDEV_PROJECTS_BASE_PATH default.
	DefaultProjectsBase = "/opt/maxicore-fc/webdev/projects"

	// DefaultCheckpointTimeout caps the time for SaveCheckpoint / RestoreProject.
	DefaultCheckpointTimeout = 10 * time.Minute

	// DefaultGitTimeout caps git subprocess operations.
	DefaultGitTimeout = 60 * time.Second
)

// ErrProjectNotFound is returned when a project directory does not exist.
var ErrProjectNotFound = errors.New("project not found")

// Service is the WebdevService Connect-RPC implementation.
//
// Concurrency: methods are safe for concurrent use. Per-project locks
// (via projectLocks) serialize SaveCheckpoint/RestoreProject/RollbackProject
// for the same project to avoid file-system races.
type Service struct {
	logger       *zerolog.Logger
	projectsBase string

	// projectLocks serializes destructive operations per-project.
	// Map of project_name → *sync.Mutex. Per-key lock pattern.
	projectLocks sync.Map

	// rcloneSync holds the active RcloneS3Sync (one per VM, shared across projects).
	// Initialized lazily on first RefreshS3Token. nil-safe.
	rcloneMu   sync.RWMutex
	rcloneSync *RcloneS3Sync
}

// Config configures a new webdev Service.
type Config struct {
	Logger       *zerolog.Logger
	ProjectsBase string // default DefaultProjectsBase if empty
}

// NewService constructs a webdev Service.
func NewService(cfg Config) (*Service, error) {
	if cfg.Logger == nil {
		return nil, errors.New("webdev.NewService: Logger is required")
	}
	base := cfg.ProjectsBase
	if base == "" {
		base = DefaultProjectsBase
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return nil, fmt.Errorf("webdev.NewService: mkdir projects-base: %w", err)
	}
	return &Service{
		logger:       cfg.Logger,
		projectsBase: base,
	}, nil
}

// projectPath returns the on-disk path for a project directory.
// If projectName is empty or contains path traversal, returns "" + error.
func (s *Service) projectPath(projectName string) (string, error) {
	if projectName == "" {
		return "", errors.New("project_name is required")
	}
	// Reject any path-traversal or absolute-path attempts before filepath.Clean
	// normalizes them away.
	if strings.ContainsRune(projectName, '/') || strings.ContainsRune(projectName, '\\') {
		return "", fmt.Errorf("invalid project_name (no path separators allowed): %q", projectName)
	}
	if projectName == "." || projectName == ".." || strings.HasPrefix(projectName, ".") {
		return "", fmt.Errorf("invalid project_name: %q", projectName)
	}
	clean := filepath.Clean(projectName)
	if clean != projectName || filepath.IsAbs(clean) {
		return "", fmt.Errorf("invalid project_name: %q", projectName)
	}
	return filepath.Join(s.projectsBase, clean), nil
}

// projectExists checks whether a project directory exists.
func (s *Service) projectExists(projectName string) (bool, error) {
	p, err := s.projectPath(projectName)
	if err != nil {
		return false, err
	}
	info, err := os.Stat(p)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return info.IsDir(), nil
}

// lockProject acquires a per-project mutex.
// Returns an unlock-function that MUST be called (typically via defer).
func (s *Service) lockProject(projectName string) func() {
	mu, _ := s.projectLocks.LoadOrStore(projectName, &sync.Mutex{})
	m := mu.(*sync.Mutex)
	m.Lock()
	return m.Unlock
}

// withCheckpointTimeout derives a context with the checkpoint default deadline,
// unless ctx already has an earlier deadline.
func withCheckpointTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if dl, ok := ctx.Deadline(); ok && time.Until(dl) < DefaultCheckpointTimeout {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, DefaultCheckpointTimeout)
}

// SetRcloneSync installs the rclone sync engine.
// Called after RefreshS3Token initializes credentials.
func (s *Service) SetRcloneSync(r *RcloneS3Sync) {
	s.rcloneMu.Lock()
	defer s.rcloneMu.Unlock()
	s.rcloneSync = r
}

// rclone returns the active RcloneS3Sync or nil if not configured.
func (s *Service) rclone() *RcloneS3Sync {
	s.rcloneMu.RLock()
	defer s.rcloneMu.RUnlock()
	return s.rcloneSync
}
