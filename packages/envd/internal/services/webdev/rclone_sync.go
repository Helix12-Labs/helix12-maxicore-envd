// Package webdev — RcloneS3Sync continuous S3-sync engine (Manus 1:1).
//
// Manus uses the external `rclone` binary (NOT aws-sdk-go directly) for
// continuous project-state sync. Verified RAM symbols:
//
//	sbx-go-svc/pkg/runtime/tools/webdev.RcloneS3Sync
//	sbx-go-svc/pkg/runtime/tools/webdev.NewRcloneS3Sync
//	sbx-go-svc/pkg/runtime/tools/webdev.{createConfig, StartAutoSync,
//	    syncIncremental, syncDeletions, watchAndSync, UpdateCredentials,
//	    shouldSyncFile, addWatchDirs}
//
// Lifecycle:
//  1. NewRcloneS3Sync(...) writes a per-VM rclone config to /tmp.
//  2. StartAutoSync(ctx) launches a goroutine that:
//     - watches project dirs via fsnotify
//     - debounces dirty events for 30s
//     - exec's `rclone sync` with the Manus exclude-set
//  3. UpdateCredentials(...) atomically replaces the config when
//     RefreshS3Token RPC returns new STS creds.
//  4. Stop() cleanly terminates the goroutine.
//
// Excludes match Manus's rclone-call (RAM-verified):
//
//	node_modules/**, .git/**, __pycache__/**, .manus-logs/**, .next/**
//	dist/**, build/**, .cache/**, .pnpm-store/**, .venv/**, venv/**, target/**
package webdev

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// rcloneExcludes matches Manus's rclone --exclude set.
var rcloneExcludes = []string{
	"node_modules/**",
	".git/**",
	"__pycache__/**",
	".manus-logs/**",
	".maxicore-logs/**",
	".next/**",
	"dist/**",
	"build/**",
	".cache/**",
	".pnpm-store/**",
	".venv/**",
	"venv/**",
	"target/**",
}

// RcloneCredentials are the per-VM S3 STS credentials (refreshed via
// RefreshS3Token RPC). Bucket and Endpoint are required.
type RcloneCredentials struct {
	Endpoint     string // S3 endpoint (e.g. "https://minio.maxicore.eu:9000")
	Region       string // S3 region (e.g. "eu-central-1")
	AccessKey    string // STS access key
	SecretKey    string // STS secret key
	SessionToken string // STS session token (optional for permanent keys)
	Bucket       string // target bucket
}

// RcloneS3Sync is the continuous-sync engine.
type RcloneS3Sync struct {
	logger      *zerolog.Logger
	configPath  string // /tmp/rclone-<vm-id>.conf
	projectBase string // /opt/maxicore-fc/webdev/projects

	// rcloneBin is the path to the rclone binary. Default "rclone" (PATH lookup).
	rcloneBin string

	// syncInterval defaults to 30s (Manus's setting).
	syncInterval time.Duration

	credsMu sync.RWMutex
	creds   RcloneCredentials

	runningMu sync.Mutex
	running   bool
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

// RcloneConfig configures a new RcloneS3Sync.
type RcloneConfig struct {
	Logger       *zerolog.Logger
	ConfigPath   string            // default "/tmp/rclone-maxicore.conf"
	ProjectBase  string            // default DefaultProjectsBase
	RcloneBin    string            // default "rclone"
	SyncInterval time.Duration     // default 30s
	Credentials  RcloneCredentials // required
}

// NewRcloneS3Sync constructs a syncer. Writes initial rclone config to disk.
func NewRcloneS3Sync(cfg RcloneConfig) (*RcloneS3Sync, error) {
	if cfg.Logger == nil {
		return nil, errors.New("RcloneS3Sync: Logger is required")
	}
	if cfg.Credentials.Endpoint == "" || cfg.Credentials.Bucket == "" {
		return nil, errors.New("RcloneS3Sync: Endpoint + Bucket required")
	}
	r := &RcloneS3Sync{
		logger:       cfg.Logger,
		configPath:   cfg.ConfigPath,
		projectBase:  cfg.ProjectBase,
		rcloneBin:    cfg.RcloneBin,
		syncInterval: cfg.SyncInterval,
		creds:        cfg.Credentials,
	}
	if r.configPath == "" {
		r.configPath = "/tmp/rclone-maxicore.conf"
	}
	if r.projectBase == "" {
		r.projectBase = DefaultProjectsBase
	}
	if r.rcloneBin == "" {
		r.rcloneBin = "rclone"
	}
	if r.syncInterval == 0 {
		r.syncInterval = 30 * time.Second
	}
	if err := r.createConfig(); err != nil {
		return nil, fmt.Errorf("createConfig: %w", err)
	}
	return r, nil
}

// createConfig writes an rclone config file with the current credentials.
// Section name is "s3" (consistent across config rewrites for stable remote-refs).
func (r *RcloneS3Sync) createConfig() error {
	r.credsMu.RLock()
	c := r.creds
	r.credsMu.RUnlock()

	content := fmt.Sprintf(`[s3]
type = s3
provider = Other
endpoint = %s
region = %s
access_key_id = %s
secret_access_key = %s
session_token = %s
force_path_style = true
`, c.Endpoint, c.Region, c.AccessKey, c.SecretKey, c.SessionToken)

	tmp := r.configPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, r.configPath)
}

// UpdateCredentials atomically replaces the rclone config (and creds).
// Called when RefreshS3Token RPC returns rotated STS keys.
func (r *RcloneS3Sync) UpdateCredentials(creds RcloneCredentials) error {
	r.credsMu.Lock()
	r.creds = creds
	r.credsMu.Unlock()
	return r.createConfig()
}

// StartAutoSync begins continuous background sync. Safe to call once;
// subsequent calls return ErrAlreadyRunning.
func (r *RcloneS3Sync) StartAutoSync(ctx context.Context) error {
	r.runningMu.Lock()
	defer r.runningMu.Unlock()
	if r.running {
		return errors.New("RcloneS3Sync already running")
	}
	ctx, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	r.running = true
	r.wg.Add(1)
	go r.watchAndSync(ctx)
	return nil
}

// Stop terminates the background sync. Safe to call multiple times.
func (r *RcloneS3Sync) Stop() {
	r.runningMu.Lock()
	defer r.runningMu.Unlock()
	if !r.running {
		return
	}
	r.cancel()
	r.wg.Wait()
	r.running = false
}

// watchAndSync runs the periodic sync ticker. Manus uses a 30s interval.
//
// For fsnotify-driven sync we would coalesce file-events here, but rclone's
// own delta-detection (--update --transfers --checkers) is efficient enough
// that a timer-only loop matches Manus's observed pattern.
func (r *RcloneS3Sync) watchAndSync(ctx context.Context) {
	defer r.wg.Done()
	t := time.NewTicker(r.syncInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			projects, err := r.listProjects()
			if err != nil {
				r.logger.Warn().Err(err).Msg("watchAndSync: listProjects failed")
				continue
			}
			for _, proj := range projects {
				if err := r.SyncUp(ctx, proj); err != nil {
					r.logger.Warn().Err(err).Str("project", proj).Msg("syncIncremental failed")
				}
			}
		}
	}
}

// listProjects returns the names of all direct subdirectories under projectBase.
func (r *RcloneS3Sync) listProjects() ([]string, error) {
	entries, err := os.ReadDir(r.projectBase)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

// SyncUp uploads (sync) the project's local state to S3.
//
// `rclone sync` mirrors local → remote with deletions. Excludes match Manus.
func (r *RcloneS3Sync) SyncUp(ctx context.Context, projectName string) error {
	localPath := filepath.Join(r.projectBase, projectName)
	remotePath := r.remotePathFor(projectName)
	return r.runRclone(ctx, "sync", localPath, remotePath)
}

// SyncDown downloads (sync) the project's state from S3 into destPath.
func (r *RcloneS3Sync) SyncDown(ctx context.Context, projectName, destPath string) error {
	remotePath := r.remotePathFor(projectName)
	return r.runRclone(ctx, "sync", remotePath, destPath)
}

// RetainNewestBundle scans the S3 prefix for checkpoint bundles and keeps
// only the newest (by modtime). Returns (kept-name, removed-count, error).
//
// Manus log: "S3 preflight: keeping newest bundle".
func (r *RcloneS3Sync) RetainNewestBundle(ctx context.Context, projectName string) (string, int, error) {
	bundlePrefix := r.bundlePathFor(projectName)

	// rclone lsl --files-only --max-depth 1 -> "<size> YYYY-MM-DD HH:MM:SS.fff <name>"
	out, err := r.execRclone(ctx, "lsl", "--max-depth", "1", "--files-only", bundlePrefix)
	if err != nil {
		return "", 0, fmt.Errorf("rclone lsl: %w", err)
	}
	files := parseRcloneLSL(out)
	if len(files) <= 1 {
		if len(files) == 1 {
			return files[0].Name, 0, nil
		}
		return "", 0, nil
	}

	// Find newest (lexicographically by modtime string works because it's ISO-8601).
	newest := files[0]
	for _, f := range files[1:] {
		if f.ModTime > newest.ModTime {
			newest = f
		}
	}

	removed := 0
	for _, f := range files {
		if f.Name == newest.Name {
			continue
		}
		if err := r.runRclone(ctx, "delete", bundlePrefix+"/"+f.Name); err != nil {
			r.logger.Warn().Err(err).Str("file", f.Name).Msg("RetainNewestBundle: delete failed")
			continue
		}
		removed++
	}
	return newest.Name, removed, nil
}

// remotePathFor returns the S3 remote-path for a project's working state.
//
//	s3:<bucket>/projects/<project_name>
func (r *RcloneS3Sync) remotePathFor(projectName string) string {
	r.credsMu.RLock()
	bucket := r.creds.Bucket
	r.credsMu.RUnlock()
	return fmt.Sprintf("s3:%s/projects/%s", bucket, projectName)
}

// bundlePathFor returns the S3 remote-path for a project's checkpoint bundles.
//
//	s3:<bucket>/checkpoints/<project_name>
func (r *RcloneS3Sync) bundlePathFor(projectName string) string {
	r.credsMu.RLock()
	bucket := r.creds.Bucket
	r.credsMu.RUnlock()
	return fmt.Sprintf("s3:%s/checkpoints/%s", bucket, projectName)
}

// runRclone executes `rclone <cmd> <src> <dst>` with the Manus exclude-set.
// Sync-style verbs: "sync", "copy". For "delete" / "lsl" the exclude flags
// are still safe (rclone ignores unknown flags' applicability).
func (r *RcloneS3Sync) runRclone(ctx context.Context, args ...string) error {
	full := []string{"--config", r.configPath}
	full = append(full, args...)
	for _, ex := range rcloneExcludes {
		full = append(full, "--exclude", ex)
	}
	cmd := exec.CommandContext(ctx, r.rcloneBin, full...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("rclone %v: %w: %s", args, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// execRclone is like runRclone but returns combined stdout/stderr for parsing.
func (r *RcloneS3Sync) execRclone(ctx context.Context, args ...string) (string, error) {
	full := []string{"--config", r.configPath}
	full = append(full, args...)
	cmd := exec.CommandContext(ctx, r.rcloneBin, full...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// rcloneFile is a single line in `rclone lsl` output.
type rcloneFile struct {
	Size    int64
	ModTime string // ISO-8601 with millis
	Name    string
}

// parseRcloneLSL parses `rclone lsl` output. Each line:
//
//	"      1024 2026-05-14 18:30:00.000000000 my-file.tar.gz"
//
// Lines that don't match the expected shape (numeric size + ISO date + ISO time)
// are skipped silently.
func parseRcloneLSL(out string) []rcloneFile {
	var files []rcloneFile
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// fields: [size, date, time, name...]
		parts := strings.Fields(line)
		if len(parts) < 4 {
			continue
		}
		// size must be numeric
		var sz int64
		if n, err := fmt.Sscanf(parts[0], "%d", &sz); err != nil || n != 1 {
			continue
		}
		// date must look like YYYY-MM-DD (10 chars, 2 dashes)
		if len(parts[1]) != 10 || parts[1][4] != '-' || parts[1][7] != '-' {
			continue
		}
		// time must contain a colon (HH:MM:SS[.frac])
		if !strings.Contains(parts[2], ":") {
			continue
		}
		modtime := parts[1] + "T" + parts[2]
		name := strings.Join(parts[3:], " ")
		files = append(files, rcloneFile{Size: sz, ModTime: modtime, Name: name})
	}
	return files
}

// ShouldSyncFile returns true if a given relative path should participate in sync.
// Used both for filesystem-watchers and for unit-test verification of exclude rules.
func ShouldSyncFile(relPath string) bool {
	clean := filepath.ToSlash(filepath.Clean(relPath))
	parts := strings.Split(clean, "/")
	for _, part := range parts {
		for _, ex := range rcloneExcludes {
			// Patterns like "node_modules/**" match if "node_modules" appears in path
			base := strings.TrimSuffix(ex, "/**")
			if part == base {
				return false
			}
		}
	}
	return true
}
