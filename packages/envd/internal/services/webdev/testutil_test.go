package webdev

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"
)

// newTestService creates a Service rooted at a temp directory.
// Returns (service, projectsBase). The temp dir is cleaned up via t.Cleanup.
func newTestService(t *testing.T) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	logger := zerolog.Nop()
	svc, err := NewService(Config{
		Logger:       &logger,
		ProjectsBase: dir,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc, dir
}

// makeProject creates a project dir with the given files under projectsBase.
// Returns the project's absolute path.
func makeProject(t *testing.T, projectsBase, projectName string, files map[string]string) string {
	t.Helper()
	pp := filepath.Join(projectsBase, projectName)
	if err := os.MkdirAll(pp, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	for rel, content := range files {
		full := filepath.Join(pp, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir parent: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return pp
}
