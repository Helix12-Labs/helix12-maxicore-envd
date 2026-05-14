package handlers

import (
	"context"
	"os"
	"time"

	"connectrpc.com/connect"

	runtimev1 "github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/services/runtime/v1"
)

// RuntimeHandler implements runtimev1connect.RuntimeServiceHandler.
//
// All 4 RPCs are implemented (trivial introspection endpoints):
//   - GetHealth   → "ok"
//   - GetVersion  → version string
//   - HasFuse     → check /dev/fuse + mount-bin existence
//   - InitSandbox → unimplemented (B.II.x.RuntimeService-InitSandbox sprint)
type RuntimeHandler struct {
	version   string
	startedAt time.Time
}

// NewRuntimeHandler constructs a RuntimeHandler.
//
// version: the envd version-string (e.g. "v0.8.0-b.ii.1d-wire").
func NewRuntimeHandler(version string) *RuntimeHandler {
	return &RuntimeHandler{
		version:   version,
		startedAt: time.Now(),
	}
}

func (h *RuntimeHandler) GetHealth(
	ctx context.Context,
	req *connect.Request[runtimev1.GetHealthRequest],
) (*connect.Response[runtimev1.GetHealthResponse], error) {
	return connect.NewResponse(&runtimev1.GetHealthResponse{
		Status: "ok",
	}), nil
}

func (h *RuntimeHandler) GetVersion(
	ctx context.Context,
	req *connect.Request[runtimev1.GetVersionRequest],
) (*connect.Response[runtimev1.GetVersionResponse], error) {
	return connect.NewResponse(&runtimev1.GetVersionResponse{
		Version: h.version,
	}), nil
}

// HasFuse probes for FUSE support: /dev/fuse exists and /usr/bin/fusermount3
// (or fusermount) is installed. The fields mirror Manus's HasFuseResponse:
//
//	dev_fuse  → "true"/"false"
//	has_fuse  → "true"/"false" (logical AND of dev_fuse + mount_bin)
//	mount_bin → path to fusermount(3) or "" if missing
func (h *RuntimeHandler) HasFuse(
	ctx context.Context,
	req *connect.Request[runtimev1.HasFuseRequest],
) (*connect.Response[runtimev1.HasFuseResponse], error) {
	devFuse := fileExists("/dev/fuse")
	mountBin := firstExisting(
		"/usr/bin/fusermount3",
		"/usr/local/bin/fusermount3",
		"/usr/bin/fusermount",
		"/usr/local/bin/fusermount",
	)
	has := devFuse && mountBin != ""
	return connect.NewResponse(&runtimev1.HasFuseResponse{
		DevFuse:  boolStr(devFuse),
		HasFuse:  boolStr(has),
		MountBin: mountBin,
	}), nil
}

func (h *RuntimeHandler) InitSandbox(
	ctx context.Context,
	req *connect.Request[runtimev1.InitSandboxRequest],
) (*connect.Response[runtimev1.InitSandboxResponse], error) {
	return nil, unimpl("RuntimeService.InitSandbox", "B.II.x.RuntimeService-InitSandbox")
}

// --- helpers ---

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func firstExisting(paths ...string) string {
	for _, p := range paths {
		if fileExists(p) {
			return p
		}
	}
	return ""
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
