package webdev

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"

	runtimev1 "github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/services/runtime/v1"
)

// TC1: happy path — RestoreProject creates project dir; no rclone, no git.
func TestRestoreProject_HappyPath_NoRcloneNoGit(t *testing.T) {
	svc, _ := newTestService(t)
	req := connect.NewRequest(&runtimev1.RestoreProjectRequest{
		ProjectName: "newproj",
	})
	resp, err := svc.RestoreProject(context.Background(), req)
	if err != nil {
		t.Fatalf("RestoreProject err: %v", err)
	}
	// Project dir must exist
	exists, _ := svc.projectExists("newproj")
	if !exists {
		t.Fatal("project dir should exist after restore")
	}
	var data map[string]any
	_ = json.Unmarshal([]byte(resp.Msg.Data), &data)
	if data["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", data["status"])
	}
	if data["rclone_synced"] != false {
		t.Errorf("expected rclone_synced=false (no rclone configured), got %v", data["rclone_synced"])
	}
}

// TC2: missing project_name → CodeInvalidArgument
func TestRestoreProject_MissingProjectName(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.RestoreProject(context.Background(), connect.NewRequest(&runtimev1.RestoreProjectRequest{}))
	if err == nil {
		t.Fatal("expected error for empty project_name")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("expected CodeInvalidArgument, got %v", connect.CodeOf(err))
	}
}

// TC3: project_config with source_git_repo triggers git clone path (we test that
// the path is attempted; cloning fails with an unreachable URL but doesn't fail RPC).
func TestRestoreProject_GitCloneAttempted_Failure_NonFatal(t *testing.T) {
	svc, _ := newTestService(t)
	cfg := projectConfig{SourceGitRepo: "https://invalid.invalid/repo.git"}
	cfgJSON, _ := json.Marshal(cfg)
	req := connect.NewRequest(&runtimev1.RestoreProjectRequest{
		ProjectName:   "withgit",
		ProjectConfig: string(cfgJSON),
	})
	resp, err := svc.RestoreProject(context.Background(), req)
	if err != nil {
		t.Fatalf("RestoreProject should not fail on git-clone error, got: %v", err)
	}
	var data map[string]any
	_ = json.Unmarshal([]byte(resp.Msg.Data), &data)
	// git_cloned must be false (URL is unreachable)
	if data["git_cloned"] != false {
		t.Errorf("expected git_cloned=false, got %v", data["git_cloned"])
	}
}

// TC4: extractCheckpointZip — tar.gz extracts correctly, rejects path-traversal
func TestExtractCheckpointZip_HappyPath(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "ckpt.tgz")
	makeTestTarball(t, zipPath, map[string]string{
		"src/main.go":       "package main",
		"package.json":      "{}",
		"deep/nested/a.txt": "hello",
	})
	dest := filepath.Join(dir, "out")
	if err := extractCheckpointZip(zipPath, dest); err != nil {
		t.Fatalf("extract: %v", err)
	}
	for _, p := range []string{"src/main.go", "package.json", "deep/nested/a.txt"} {
		fp := filepath.Join(dest, p)
		if _, err := os.Stat(fp); err != nil {
			t.Errorf("expected %q to exist: %v", p, err)
		}
	}
}

// TC5: extractCheckpointZip rejects path-traversal entries
func TestExtractCheckpointZip_RejectsPathTraversal(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "evil.tgz")
	makeEvilTarball(t, zipPath, "../escaped.txt", []byte("evil"))
	dest := filepath.Join(dir, "out")
	err := extractCheckpointZip(zipPath, dest)
	if err == nil {
		t.Fatal("expected error for path-traversal tarball")
	}
}

// TC6: downloadFromURL HTTP success
func TestDownloadFromURL_Success(t *testing.T) {
	want := []byte("hello world")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(want)
	}))
	defer srv.Close()
	dest := filepath.Join(t.TempDir(), "dl.bin")
	if err := downloadFromURL(context.Background(), srv.URL, dest); err != nil {
		t.Fatalf("download: %v", err)
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, want) {
		t.Errorf("got %q want %q", got, want)
	}
}

// TC7: downloadFromURL HTTP 500 → error
func TestDownloadFromURL_HTTP500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	dest := filepath.Join(t.TempDir(), "dl.bin")
	err := downloadFromURL(context.Background(), srv.URL, dest)
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
}

// TC8: isDirEmpty cases
func TestIsDirEmpty(t *testing.T) {
	dir := t.TempDir()
	empty, err := isDirEmpty(dir)
	if err != nil || !empty {
		t.Fatalf("new tempdir should be empty: empty=%v err=%v", empty, err)
	}
	os.WriteFile(filepath.Join(dir, "x"), []byte("y"), 0o644)
	empty, err = isDirEmpty(dir)
	if err != nil || empty {
		t.Fatalf("after write should not be empty: empty=%v err=%v", empty, err)
	}
	empty, err = isDirEmpty(filepath.Join(dir, "missing"))
	if err != nil || !empty {
		t.Fatalf("missing dir should be empty=true err=nil, got empty=%v err=%v", empty, err)
	}
}

// makeTestTarball creates a tar.gz with given files (rel-path → content).
func makeTestTarball(t *testing.T, zipPath string, files map[string]string) {
	t.Helper()
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gzw := gzip.NewWriter(f)
	tw := tar.NewWriter(gzw)
	for name, content := range files {
		hdr := &tar.Header{
			Name:     name,
			Mode:     0o644,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(tw, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatal(err)
	}
}

// makeEvilTarball creates a tar.gz with a single path-traversal entry.
func makeEvilTarball(t *testing.T, zipPath, evilName string, content []byte) {
	t.Helper()
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gzw := gzip.NewWriter(f)
	tw := tar.NewWriter(gzw)
	hdr := &tar.Header{
		Name:     evilName,
		Mode:     0o644,
		Size:     int64(len(content)),
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gzw.Close()
}
