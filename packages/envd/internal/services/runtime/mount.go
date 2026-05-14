// Package runtime mounts the runtime.v1.* Connect-RPC service surface onto an
// existing chi router. This is the single entry-point used by main.go to wire
// up the in-VM RPC layer.
//
// Wiring (Manus 1:1):
//
//	+-----------------+    HTTP/2 + JSON (snake_case)
//	|  Backend / SDK  | -------------------------+
//	+-----------------+                          |
//	                                             v
//	                          +------------------------------------+
//	                          | HMAC-V2 middleware (X-Sandbox-Api-*)|
//	                          +------------------------------------+
//	                                             |
//	                                             v
//	                          +------------------------------------+
//	                          | runtime.v1.* Connect-RPC handlers  |
//	                          | + SnakeCaseJSONCodec               |
//	                          +------------------------------------+
//
// Authorization policy:
//
//   - RuntimeService, ConfigService, BrowserService, TerminalService,
//     FileService, SlideService, DeployService, EmailService, SkillService,
//     ImageService, TextEditorService → RequireAuth (60s window, nonce-replay).
//   - WebdevService → RequireAuthSkipTimeCheck (replay-only). Long-running
//     ops (SaveCheckpoint, RestoreProject, GitPush) can exceed the time window.
//
// Codec: All services emit snake_case JSON (Manus wire format) via
// codec.SnakeCaseJSONCodec, registered with the Connect-RPC handler factories.
package runtime

import (
	"errors"
	"net/http"

	"connectrpc.com/connect"
	"github.com/go-chi/chi/v5"

	"github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/auth"
	"github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/codec"
	"github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/services/runtime/handlers"
	"github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/services/runtime/v1/runtimev1connect"
	"github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/services/webdev"
)

// Deps is the dependency-bundle for mounting the runtime.v1 surface.
type Deps struct {
	// Auth must be a fully-initialized HMAC-V2 middleware (with secret).
	// May be nil ONLY if SkipAuth is true (development/testing).
	Auth *auth.Middleware

	// WebdevSvc is the production webdev.Service backing the 4 implemented
	// WebdevService RPCs (SaveCheckpoint/RestoreProject/RollbackProject/RestartProject).
	// Required.
	WebdevSvc *webdev.Service

	// Version is exposed via RuntimeService.GetVersion.
	Version string

	// SkipAuth disables the HMAC-V2 wrapper. Use ONLY in tests or development.
	// In production this MUST be false; main.go should refuse to start with
	// SkipAuth=true unless an explicit dev-flag is set.
	SkipAuth bool
}

// Validate returns an error if Deps is missing required fields.
func (d *Deps) Validate() error {
	if d.WebdevSvc == nil {
		return errors.New("runtime.Mount: WebdevSvc is required")
	}
	if !d.SkipAuth && d.Auth == nil {
		return errors.New("runtime.Mount: Auth is required (or set SkipAuth=true for dev)")
	}
	return nil
}

// Mount registers all 12 runtime.v1 Connect-RPC services on the given chi mux.
//
// Each service is wrapped with the appropriate auth-middleware variant. The
// SnakeCaseJSONCodec is registered as the JSON codec so the wire format
// matches Manus 1:1.
//
// Returns the list of mounted Connect-RPC route prefixes for logging / debug.
func Mount(mux *chi.Mux, deps *Deps) ([]string, error) {
	if err := deps.Validate(); err != nil {
		return nil, err
	}

	// Connect-RPC handler options: register snake_case codec.
	opts := []connect.HandlerOption{
		connect.WithCodec(&codec.SnakeCaseJSONCodec{}),
	}

	// Build WebdevHandler from the webdev.Service.
	webdevH, err := handlers.NewWebdevHandler(deps.WebdevSvc)
	if err != nil {
		return nil, err
	}

	// Build all 12 service handlers + paths.
	type svc struct {
		Path    string
		Handler http.Handler
		Long    bool // true → use RequireAuthSkipTimeCheck (long-running ops)
	}

	runtimePath, runtimeH := runtimev1connect.NewRuntimeServiceHandler(
		handlers.NewRuntimeHandler(deps.Version), opts...)
	configPath, configH := runtimev1connect.NewConfigServiceHandler(
		handlers.ConfigStubHandler{}, opts...)
	browserPath, browserH := runtimev1connect.NewBrowserServiceHandler(
		handlers.BrowserStubHandler{}, opts...)
	terminalPath, terminalH := runtimev1connect.NewTerminalServiceHandler(
		handlers.TerminalStubHandler{}, opts...)
	filePath, fileH := runtimev1connect.NewFileServiceHandler(
		handlers.FileStubHandler{}, opts...)
	slidePath, slideH := runtimev1connect.NewSlideServiceHandler(
		handlers.SlideStubHandler{}, opts...)
	webdevPath, webdevHandler := runtimev1connect.NewWebdevServiceHandler(
		webdevH, opts...)
	emailPath, emailH := runtimev1connect.NewEmailServiceHandler(
		handlers.EmailStubHandler{}, opts...)
	deployPath, deployH := runtimev1connect.NewDeployServiceHandler(
		handlers.DeployStubHandler{}, opts...)
	skillPath, skillH := runtimev1connect.NewSkillServiceHandler(
		handlers.SkillStubHandler{}, opts...)
	imagePath, imageH := runtimev1connect.NewImageServiceHandler(
		handlers.ImageStubHandler{}, opts...)
	textEditorPath, textEditorH := runtimev1connect.NewTextEditorServiceHandler(
		handlers.TextEditorStubHandler{}, opts...)

	services := []svc{
		{runtimePath, runtimeH, false},
		{configPath, configH, false},
		{browserPath, browserH, false},
		{terminalPath, terminalH, false},
		{filePath, fileH, false},
		{slidePath, slideH, false},
		{webdevPath, webdevHandler, true}, // long-running ops
		{emailPath, emailH, false},
		{deployPath, deployH, false},
		{skillPath, skillH, false},
		{imagePath, imageH, false},
		{textEditorPath, textEditorH, false},
	}

	mounted := make([]string, 0, len(services))
	for _, s := range services {
		var wrapped http.Handler = s.Handler
		if !deps.SkipAuth {
			if s.Long {
				wrapped = deps.Auth.RequireAuthSkipTimeCheck(s.Handler)
			} else {
				wrapped = deps.Auth.RequireAuth(s.Handler)
			}
		}
		mux.Mount(s.Path, wrapped)
		mounted = append(mounted, s.Path)
	}
	return mounted, nil
}
