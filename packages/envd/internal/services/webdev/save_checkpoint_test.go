package webdev

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"connectrpc.com/connect"

	runtimev1 "github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/services/runtime/v1"
)

// newCapturingS3 spins up a httptest server that accepts a PUT and stores body+status.
type capturedUpload struct {
	body        []byte
	contentType string
	statusCode  int
	hits        int
}

func newCapturingS3(t *testing.T, respondStatus int) (*httptest.Server, *capturedUpload) {
	t.Helper()
	cu := &capturedUpload{statusCode: respondStatus}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cu.hits++
		cu.contentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		cu.body = body
		w.WriteHeader(respondStatus)
	}))
	t.Cleanup(srv.Close)
	return srv, cu
}

// TC1: happy path — checkpoint, upload, response with last_checkpoint_commit absent (no git).
func TestSaveCheckpoint_HappyPath_NoGit(t *testing.T) {
	svc, base := newTestService(t)
	makeProject(t, base, "p1", map[string]string{
		"src/index.ts":   "console.log('hi');\n",
		"package.json":   "{}",
		"node_modules/x": "should be excluded",
	})
	srv, cu := newCapturingS3(t, http.StatusOK)

	req := connect.NewRequest(&runtimev1.SaveCheckpointRequest{
		ProjectName:            "p1",
		CheckpointZipUploadUrl: srv.URL + "/bucket/p1.tgz",
		Description:            "v1",
	})
	resp, err := svc.SaveCheckpoint(context.Background(), req)
	if err != nil {
		t.Fatalf("SaveCheckpoint err: %v", err)
	}
	if cu.hits != 1 {
		t.Fatalf("expected 1 upload, got %d", cu.hits)
	}
	if cu.contentType != "application/gzip" {
		t.Errorf("expected content-type application/gzip, got %q", cu.contentType)
	}

	var data map[string]any
	if err := json.Unmarshal([]byte(resp.Msg.Data), &data); err != nil {
		t.Fatalf("response data not JSON: %v: %s", err, resp.Msg.Data)
	}
	if data["status"] != "ok" {
		t.Errorf("expected status=ok, got %v", data["status"])
	}
	if data["project_name"] != "p1" {
		t.Errorf("expected project_name=p1, got %v", data["project_name"])
	}
	if commit, ok := data["last_checkpoint_commit"]; !ok || commit != "" {
		t.Errorf("expected empty last_checkpoint_commit, got %v", commit)
	}

	// Verify the tar.gz body excludes node_modules
	verifyTarballExcludes(t, cu.body, []string{"node_modules"})
	verifyTarballIncludes(t, cu.body, []string{"src/index.ts", "package.json"})
}

// TC2: project_name missing → CodeInvalidArgument
func TestSaveCheckpoint_MissingProjectName(t *testing.T) {
	svc, _ := newTestService(t)
	req := connect.NewRequest(&runtimev1.SaveCheckpointRequest{
		CheckpointZipUploadUrl: "https://x/y",
	})
	_, err := svc.SaveCheckpoint(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for missing project_name")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("expected CodeInvalidArgument, got %v", connect.CodeOf(err))
	}
}

// TC3: upload URL missing → CodeInvalidArgument
func TestSaveCheckpoint_MissingUploadURL(t *testing.T) {
	svc, base := newTestService(t)
	makeProject(t, base, "p1", map[string]string{"a.txt": "x"})
	req := connect.NewRequest(&runtimev1.SaveCheckpointRequest{
		ProjectName: "p1",
	})
	_, err := svc.SaveCheckpoint(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for missing upload URL")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("expected CodeInvalidArgument, got %v", connect.CodeOf(err))
	}
}

// TC4: project does not exist → CodeNotFound
func TestSaveCheckpoint_ProjectNotFound(t *testing.T) {
	svc, _ := newTestService(t)
	srv, _ := newCapturingS3(t, http.StatusOK)
	req := connect.NewRequest(&runtimev1.SaveCheckpointRequest{
		ProjectName:            "missing",
		CheckpointZipUploadUrl: srv.URL,
	})
	_, err := svc.SaveCheckpoint(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for missing project")
	}
	if connect.CodeOf(err) != connect.CodeNotFound {
		t.Errorf("expected CodeNotFound, got %v", connect.CodeOf(err))
	}
}

// TC5: S3 returns 500 → CodeInternal
func TestSaveCheckpoint_S3Returns500(t *testing.T) {
	svc, base := newTestService(t)
	makeProject(t, base, "p1", map[string]string{"a.txt": "x"})
	srv, _ := newCapturingS3(t, http.StatusInternalServerError)

	req := connect.NewRequest(&runtimev1.SaveCheckpointRequest{
		ProjectName:            "p1",
		CheckpointZipUploadUrl: srv.URL,
	})
	_, err := svc.SaveCheckpoint(context.Background(), req)
	if err == nil {
		t.Fatal("expected error from S3 500")
	}
	if connect.CodeOf(err) != connect.CodeInternal {
		t.Errorf("expected CodeInternal, got %v", connect.CodeOf(err))
	}
}

// TC6: project_name with path traversal rejected
func TestSaveCheckpoint_PathTraversal(t *testing.T) {
	svc, _ := newTestService(t)
	for _, bad := range []string{"../etc", "/abs", "..", "."} {
		t.Run(bad, func(t *testing.T) {
			req := connect.NewRequest(&runtimev1.SaveCheckpointRequest{
				ProjectName:            bad,
				CheckpointZipUploadUrl: "https://x",
			})
			_, err := svc.SaveCheckpoint(context.Background(), req)
			if err == nil {
				t.Fatalf("expected error for %q", bad)
			}
			if connect.CodeOf(err) != connect.CodeInvalidArgument {
				t.Errorf("expected CodeInvalidArgument, got %v", connect.CodeOf(err))
			}
		})
	}
}

// TC7: shouldExclude unit test
func TestShouldExclude(t *testing.T) {
	cases := map[string]bool{
		"src/index.ts":          false,
		"node_modules/foo":      true,
		"deeply/node_modules/x": true,
		".git/HEAD":             true,
		"__pycache__/a.pyc":     true,
		".manus-logs/a":         true,
		"target/release/main":   true,
		"main.go":               false,
		"":                      false,
		".":                     false,
	}
	for path, want := range cases {
		if got := shouldExclude(path); got != want {
			t.Errorf("shouldExclude(%q) = %v, want %v", path, got, want)
		}
	}
}

// verifyTarballExcludes asserts that none of the unwanted paths appear in the tar.gz.
func verifyTarballExcludes(t *testing.T, gzipped []byte, unwanted []string) {
	t.Helper()
	names := readTarballNames(t, gzipped)
	for _, u := range unwanted {
		for _, n := range names {
			if strings.HasPrefix(n, u) || strings.Contains(n, "/"+u+"/") || strings.HasSuffix(n, "/"+u) {
				t.Errorf("tarball should NOT include %q, but found %q", u, n)
			}
		}
	}
}

// verifyTarballIncludes asserts that all the wanted paths appear in the tar.gz.
func verifyTarballIncludes(t *testing.T, gzipped []byte, wanted []string) {
	t.Helper()
	names := readTarballNames(t, gzipped)
	for _, w := range wanted {
		found := false
		for _, n := range names {
			if n == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("tarball should include %q, names: %v", w, names)
		}
	}
}

// readTarballNames returns all file/dir names inside the gzipped tarball.
func readTarballNames(t *testing.T, gzipped []byte) []string {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "x.tgz")
	if err := os.WriteFile(tmp, gzipped, 0o644); err != nil {
		t.Fatalf("write tmp: %v", err)
	}
	f, err := os.Open(tmp)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	gzr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	defer gzr.Close()
	tr := tar.NewReader(gzr)
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		names = append(names, hdr.Name)
	}
	return names
}
