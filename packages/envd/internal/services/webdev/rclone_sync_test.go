package webdev

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

// TC1: NewRcloneS3Sync writes a valid config file
func TestNewRcloneS3Sync_WritesConfig(t *testing.T) {
	dir := t.TempDir()
	logger := zerolog.Nop()
	cfgPath := filepath.Join(dir, "rclone.conf")

	r, err := NewRcloneS3Sync(RcloneConfig{
		Logger:     &logger,
		ConfigPath: cfgPath,
		Credentials: RcloneCredentials{
			Endpoint:  "https://minio.example.eu:9000",
			Region:    "eu-central-1",
			AccessKey: "AK",
			SecretKey: "SK",
			Bucket:    "test-bucket",
		},
	})
	if err != nil {
		t.Fatalf("NewRcloneS3Sync: %v", err)
	}
	defer r.Stop()

	content, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	want := []string{
		"[s3]",
		"type = s3",
		"endpoint = https://minio.example.eu:9000",
		"region = eu-central-1",
		"access_key_id = AK",
		"secret_access_key = SK",
		"force_path_style = true",
	}
	for _, w := range want {
		if !strings.Contains(string(content), w) {
			t.Errorf("rclone.conf missing %q\nGot:\n%s", w, content)
		}
	}
	// File perms must be 0600 (secret material)
	info, _ := os.Stat(cfgPath)
	if info.Mode().Perm() != 0o600 {
		t.Errorf("rclone.conf perms = %v, want 0600", info.Mode().Perm())
	}
}

// TC2: NewRcloneS3Sync rejects missing Endpoint or Bucket
func TestNewRcloneS3Sync_RequiresEndpointAndBucket(t *testing.T) {
	logger := zerolog.Nop()
	_, err := NewRcloneS3Sync(RcloneConfig{
		Logger:      &logger,
		Credentials: RcloneCredentials{Endpoint: "", Bucket: "b"},
	})
	if err == nil {
		t.Fatal("expected error for missing Endpoint")
	}
	_, err = NewRcloneS3Sync(RcloneConfig{
		Logger:      &logger,
		Credentials: RcloneCredentials{Endpoint: "https://x", Bucket: ""},
	})
	if err == nil {
		t.Fatal("expected error for missing Bucket")
	}
}

// TC3: UpdateCredentials atomically rewrites config
func TestRcloneS3Sync_UpdateCredentials(t *testing.T) {
	dir := t.TempDir()
	logger := zerolog.Nop()
	cfgPath := filepath.Join(dir, "rclone.conf")

	r, err := NewRcloneS3Sync(RcloneConfig{
		Logger:     &logger,
		ConfigPath: cfgPath,
		Credentials: RcloneCredentials{
			Endpoint: "https://old", Region: "r1", AccessKey: "AK1", SecretKey: "SK1", Bucket: "b1",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Stop()

	err = r.UpdateCredentials(RcloneCredentials{
		Endpoint: "https://new", Region: "r2", AccessKey: "AK2", SecretKey: "SK2", Bucket: "b1",
	})
	if err != nil {
		t.Fatalf("UpdateCredentials: %v", err)
	}
	content, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(content), "endpoint = https://new") {
		t.Errorf("expected rotated endpoint, got: %s", content)
	}
	if !strings.Contains(string(content), "access_key_id = AK2") {
		t.Errorf("expected rotated AK, got: %s", content)
	}
}

// TC4: ShouldSyncFile excludes the Manus exclude-set
func TestShouldSyncFile(t *testing.T) {
	cases := map[string]bool{
		"src/index.ts":               true,
		"package.json":               true,
		"node_modules/foo/index.js":  false,
		"deep/node_modules/x":        false,
		".git/HEAD":                  false,
		"src/__pycache__/cached.pyc": false,
		".next/server/page.js":       false,
		"dist/bundle.js":             false,
		".manus-logs/x":              false,
		"target/release/foo":         false,
		"build/x":                    false,
	}
	for path, want := range cases {
		if got := ShouldSyncFile(path); got != want {
			t.Errorf("ShouldSyncFile(%q) = %v, want %v", path, got, want)
		}
	}
}

// TC5: parseRcloneLSL parses standard rclone output
func TestParseRcloneLSL(t *testing.T) {
	out := `      1024 2026-05-14 18:30:00.000000000 a.tar.gz
      2048 2026-05-14 19:00:00.000000000 b.tar.gz
        12 2026-05-15 10:00:00.000000000 c.tar.gz
`
	files := parseRcloneLSL(out)
	if len(files) != 3 {
		t.Fatalf("expected 3 files, got %d", len(files))
	}
	if files[0].Size != 1024 || files[0].Name != "a.tar.gz" {
		t.Errorf("file[0]: %+v", files[0])
	}
	if files[2].Size != 12 || files[2].Name != "c.tar.gz" {
		t.Errorf("file[2]: %+v", files[2])
	}
	// Newest should be selectable lexicographically
	newest := files[0]
	for _, f := range files[1:] {
		if f.ModTime > newest.ModTime {
			newest = f
		}
	}
	if newest.Name != "c.tar.gz" {
		t.Errorf("expected newest=c.tar.gz, got %q", newest.Name)
	}
}

// TC6: parseRcloneLSL handles empty + malformed lines gracefully
func TestParseRcloneLSL_Empty(t *testing.T) {
	if got := parseRcloneLSL(""); len(got) != 0 {
		t.Errorf("expected 0 from empty, got %d", len(got))
	}
	if got := parseRcloneLSL("not a valid line\n"); len(got) != 0 {
		t.Errorf("expected 0 from malformed, got %d", len(got))
	}
}

// TC7: remotePathFor + bundlePathFor format correctly
func TestRcloneS3Sync_RemotePathsHelpers(t *testing.T) {
	dir := t.TempDir()
	logger := zerolog.Nop()
	r, err := NewRcloneS3Sync(RcloneConfig{
		Logger:     &logger,
		ConfigPath: filepath.Join(dir, "r.conf"),
		Credentials: RcloneCredentials{
			Endpoint: "https://x", Bucket: "my-bucket",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Stop()

	if got := r.remotePathFor("p1"); got != "s3:my-bucket/projects/p1" {
		t.Errorf("remotePathFor: got %q", got)
	}
	if got := r.bundlePathFor("p1"); got != "s3:my-bucket/checkpoints/p1" {
		t.Errorf("bundlePathFor: got %q", got)
	}
}

// TC8: StartAutoSync + Stop lifecycle (no real rclone invocation due to short context)
func TestRcloneS3Sync_StartStop(t *testing.T) {
	dir := t.TempDir()
	logger := zerolog.Nop()
	r, err := NewRcloneS3Sync(RcloneConfig{
		Logger:       &logger,
		ConfigPath:   filepath.Join(dir, "r.conf"),
		ProjectBase:  dir, // empty dir → no projects → no rclone exec
		SyncInterval: 50,  // short to avoid real cron tick during test
		Credentials: RcloneCredentials{
			Endpoint: "https://x", Bucket: "b",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := r.StartAutoSync(ctx); err != nil {
		t.Fatalf("StartAutoSync: %v", err)
	}
	// Second StartAutoSync must error
	if err := r.StartAutoSync(ctx); err == nil {
		t.Fatal("expected error on duplicate StartAutoSync")
	}
	r.Stop()
	// Idempotent
	r.Stop()
}
