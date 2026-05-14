package webdev

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"

	runtimev1 "github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/services/runtime/v1"
)

// TC1: RollbackProject — missing project_name → CodeInvalidArgument
func TestRollbackProject_MissingProjectName(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.RollbackProject(context.Background(), connect.NewRequest(&runtimev1.RollbackProjectRequest{
		VersionId: "abc123def",
	}))
	if err == nil || connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("expected CodeInvalidArgument, got %v", err)
	}
}

// TC2: RollbackProject — neither version_id nor zip → CodeInvalidArgument
func TestRollbackProject_NoPath(t *testing.T) {
	svc, base := newTestService(t)
	makeProject(t, base, "p1", map[string]string{"a.txt": "x"})
	_, err := svc.RollbackProject(context.Background(), connect.NewRequest(&runtimev1.RollbackProjectRequest{
		ProjectName: "p1",
	}))
	if err == nil || connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("expected CodeInvalidArgument, got %v", err)
	}
}

// TC3: RollbackProject — zip-extract path overwrites existing files
func TestRollbackProject_ZipExtract_Overwrites(t *testing.T) {
	svc, base := newTestService(t)
	projectDir := makeProject(t, base, "p1", map[string]string{
		"old.txt": "stale content",
	})

	// Build a checkpoint tarball with NEW content
	zipPath := filepath.Join(t.TempDir(), "ckpt.tgz")
	makeTestTarball(t, zipPath, map[string]string{
		"new.txt": "fresh content",
		"main.go": "package main",
	})
	// Serve it via httptest
	zipBytes, _ := os.ReadFile(zipPath)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipBytes)
	}))
	defer srv.Close()

	resp, err := svc.RollbackProject(context.Background(), connect.NewRequest(&runtimev1.RollbackProjectRequest{
		ProjectName:      "p1",
		CheckpointZipUrl: srv.URL,
		VersionId:        "non-sha-token", // force zip path
	}))
	if err != nil {
		t.Fatalf("RollbackProject: %v", err)
	}
	// old.txt must be gone (cleanProjectExceptGit), new files present
	if _, err := os.Stat(filepath.Join(projectDir, "old.txt")); !os.IsNotExist(err) {
		t.Errorf("expected old.txt removed, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(projectDir, "new.txt")); err != nil {
		t.Errorf("expected new.txt extracted: %v", err)
	}
	if resp.Msg.Data == "" {
		t.Error("expected non-empty Data")
	}
}

// TC4: isLikelyGitSHA edge cases
func TestIsLikelyGitSHA(t *testing.T) {
	cases := map[string]bool{
		"abc1234": true,
		"a1b2c3d": true,
		"abcdef0123456789abcdef0123456789abcdef01": true, // 40 chars
		"abcdef0":   true,
		"ABCDEF":    false, // 6 chars (< 7)
		"abc":       false,
		"not-a-sha": false,
		"abcdef0123456789abcdef0123456789abcdef0123": false, // > 40
		"abcg123": false, // non-hex 'g'
	}
	for s, want := range cases {
		if got := isLikelyGitSHA(s); got != want {
			t.Errorf("isLikelyGitSHA(%q) = %v, want %v", s, got, want)
		}
	}
}

// TC5: RestartProject — missing project_name → CodeInvalidArgument
func TestRestartProject_MissingProjectName(t *testing.T) {
	svc, _ := newTestService(t)
	_, err := svc.RestartProject(context.Background(), connect.NewRequest(&runtimev1.RestartProjectRequest{}))
	if err == nil || connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("expected CodeInvalidArgument, got %v", err)
	}
}

// TC6: RestartProject — missing dev_command → CodeFailedPrecondition
func TestRestartProject_MissingDevCommand(t *testing.T) {
	svc, base := newTestService(t)
	makeProject(t, base, "p1", map[string]string{"a.txt": "x"})
	_, err := svc.RestartProject(context.Background(), connect.NewRequest(&runtimev1.RestartProjectRequest{
		ProjectName:   "p1",
		ProjectConfig: `{}`,
	}))
	if err == nil || connect.CodeOf(err) != connect.CodeFailedPrecondition {
		t.Fatalf("expected CodeFailedPrecondition, got %v", err)
	}
}

// TC7: writePIDFile + stopByPIDFile basic lifecycle (uses current process)
func TestWritePIDFile_StopMissing(t *testing.T) {
	dir := t.TempDir()
	pidFile := filepath.Join(dir, "x.pid")

	// Stop on missing pidfile: returns (0, "none", nil)
	pid, method, err := stopByPIDFile(pidFile, 0)
	if err != nil || pid != 0 || method != "none" {
		t.Fatalf("missing pidfile: pid=%d method=%q err=%v", pid, method, err)
	}

	// Write our own PID, then stop with a dummy "dead pid" by overwriting
	if err := writePIDFile(pidFile, 99999999); err != nil {
		t.Fatalf("writePIDFile: %v", err)
	}
	data, err := os.ReadFile(pidFile)
	if err != nil || string(data) != "99999999" {
		t.Fatalf("pid-file content: data=%q err=%v", data, err)
	}
}
