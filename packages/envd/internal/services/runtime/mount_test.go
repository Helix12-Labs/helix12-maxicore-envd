package runtime

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/auth"
	"github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/codec"
	runtimev1 "github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/services/runtime/v1"
	"github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/services/runtime/v1/runtimev1connect"
	"github.com/Helix12-Labs/helix12-maxicore-envd/packages/envd/internal/services/webdev"
)

// httpBodyReadCloser returns an io.ReadCloser around a []byte.
func httpBodyReadCloser(b []byte) io.ReadCloser {
	return io.NopCloser(bytes.NewReader(b))
}

// newTestDeps builds a Deps bundle with a generated HMAC secret + an in-memory
// webdev.Service. Returns deps + the raw HMAC secret bytes for client signing.
func newTestDeps(t *testing.T) (*Deps, []byte) {
	t.Helper()
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		t.Fatalf("rand: %v", err)
	}
	logger := zerolog.Nop()
	svc, err := webdev.NewService(webdev.Config{
		Logger:       &logger,
		ProjectsBase: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("webdev.NewService: %v", err)
	}
	return &Deps{
		Auth:      auth.NewMiddleware(secret, 60*time.Second),
		WebdevSvc: svc,
		Version:   "v0.8.0-test",
	}, secret
}

// TC1: Mount returns the expected 12 service paths.
func TestMount_RegistersAll12Services(t *testing.T) {
	deps, _ := newTestDeps(t)
	mux := chi.NewRouter()
	mounted, err := Mount(mux, deps)
	if err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if len(mounted) != 12 {
		t.Fatalf("expected 12 services mounted, got %d: %v", len(mounted), mounted)
	}
	// Verify each well-known service prefix is present
	expected := []string{
		"/runtime.v1.RuntimeService/",
		"/runtime.v1.ConfigService/",
		"/runtime.v1.BrowserService/",
		"/runtime.v1.TerminalService/",
		"/runtime.v1.FileService/",
		"/runtime.v1.SlideService/",
		"/runtime.v1.WebdevService/",
		"/runtime.v1.EmailService/",
		"/runtime.v1.DeployService/",
		"/runtime.v1.SkillService/",
		"/runtime.v1.ImageService/",
		"/runtime.v1.TextEditorService/",
	}
	for _, e := range expected {
		found := false
		for _, m := range mounted {
			if m == e {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected service path %q in mounted: %v", e, mounted)
		}
	}
}

// TC2: Mount returns error when WebdevSvc is nil.
func TestMount_MissingWebdevSvc(t *testing.T) {
	deps := &Deps{Auth: auth.NewMiddleware([]byte("x"), time.Minute)}
	_, err := Mount(chi.NewRouter(), deps)
	if err == nil {
		t.Fatal("expected error for missing WebdevSvc")
	}
}

// TC3: Mount returns error when Auth is nil and SkipAuth is false.
func TestMount_MissingAuth(t *testing.T) {
	logger := zerolog.Nop()
	svc, _ := webdev.NewService(webdev.Config{Logger: &logger, ProjectsBase: t.TempDir()})
	deps := &Deps{WebdevSvc: svc, SkipAuth: false}
	_, err := Mount(chi.NewRouter(), deps)
	if err == nil {
		t.Fatal("expected error for nil Auth with SkipAuth=false")
	}
}

// TC4: SkipAuth=true bypasses auth middleware (for dev/test only).
func TestMount_SkipAuth_AllowsUnsignedRequests(t *testing.T) {
	logger := zerolog.Nop()
	svc, _ := webdev.NewService(webdev.Config{Logger: &logger, ProjectsBase: t.TempDir()})
	deps := &Deps{WebdevSvc: svc, SkipAuth: true, Version: "v0.8.0-test"}

	mux := chi.NewRouter()
	if _, err := Mount(mux, deps); err != nil {
		t.Fatalf("Mount: %v", err)
	}

	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Connect-RPC client call WITHOUT auth headers — must succeed due to SkipAuth
	client := runtimev1connect.NewRuntimeServiceClient(
		srv.Client(),
		srv.URL,
		connect.WithCodec(&codec.SnakeCaseJSONCodec{}),
	)
	resp, err := client.GetHealth(context.Background(), connect.NewRequest(&runtimev1.GetHealthRequest{}))
	if err != nil {
		t.Fatalf("GetHealth: %v", err)
	}
	if resp.Msg.Status != "ok" {
		t.Errorf("expected status=ok, got %q", resp.Msg.Status)
	}
}

// TC5: Authenticated GetVersion via signed request returns version string.
func TestMount_AuthenticatedGetVersion(t *testing.T) {
	deps, secret := newTestDeps(t)
	mux := chi.NewRouter()
	if _, err := Mount(mux, deps); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Build a signed Connect-RPC client by injecting headers via http.RoundTripper
	httpClient := &http.Client{
		Transport: &signingTransport{secret: secret, base: srv.Client().Transport},
	}
	client := runtimev1connect.NewRuntimeServiceClient(
		httpClient,
		srv.URL,
		connect.WithCodec(&codec.SnakeCaseJSONCodec{}),
	)
	resp, err := client.GetVersion(context.Background(), connect.NewRequest(&runtimev1.GetVersionRequest{}))
	if err != nil {
		t.Fatalf("GetVersion: %v", err)
	}
	if resp.Msg.Version != "v0.8.0-test" {
		t.Errorf("expected v0.8.0-test, got %q", resp.Msg.Version)
	}
}

// TC6: Unauthenticated request to protected endpoint returns 401.
func TestMount_UnsignedRequest_Rejected(t *testing.T) {
	deps, _ := newTestDeps(t)
	mux := chi.NewRouter()
	if _, err := Mount(mux, deps); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Client without signing transport
	client := runtimev1connect.NewRuntimeServiceClient(
		srv.Client(),
		srv.URL,
		connect.WithCodec(&codec.SnakeCaseJSONCodec{}),
	)
	_, err := client.GetHealth(context.Background(), connect.NewRequest(&runtimev1.GetHealthRequest{}))
	if err == nil {
		t.Fatal("expected error for unsigned request")
	}
	// connect-go wraps HTTP 401 as CodeUnauthenticated
	if connect.CodeOf(err) != connect.CodeUnauthenticated && connect.CodeOf(err) != connect.CodeUnknown {
		t.Errorf("expected CodeUnauthenticated, got %v: %v", connect.CodeOf(err), err)
	}
}

// TC7: Unimplemented stub returns CodeUnimplemented with sprint-tag.
func TestMount_StubReturnsUnimplemented(t *testing.T) {
	deps, secret := newTestDeps(t)
	mux := chi.NewRouter()
	if _, err := Mount(mux, deps); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	httpClient := &http.Client{
		Transport: &signingTransport{secret: secret, base: srv.Client().Transport},
	}
	client := runtimev1connect.NewSkillServiceClient(
		httpClient, srv.URL, connect.WithCodec(&codec.SnakeCaseJSONCodec{}),
	)
	_, err := client.PackageSkill(context.Background(), connect.NewRequest(&runtimev1.PackageSkillRequest{SkillName: "x"}))
	if err == nil {
		t.Fatal("expected unimplemented error")
	}
	if connect.CodeOf(err) != connect.CodeUnimplemented {
		t.Errorf("expected CodeUnimplemented, got %v: %v", connect.CodeOf(err), err)
	}
}

// TC8: WebdevService.SaveCheckpoint with missing fields → CodeInvalidArgument.
// Verifies WebdevHandler delegates correctly through the mount.
func TestMount_WebdevSaveCheckpoint_InvalidArg(t *testing.T) {
	deps, secret := newTestDeps(t)
	mux := chi.NewRouter()
	if _, err := Mount(mux, deps); err != nil {
		t.Fatalf("Mount: %v", err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	httpClient := &http.Client{
		Transport: &signingTransport{secret: secret, base: srv.Client().Transport},
	}
	client := runtimev1connect.NewWebdevServiceClient(
		httpClient, srv.URL, connect.WithCodec(&codec.SnakeCaseJSONCodec{}),
	)
	// Missing project_name
	_, err := client.SaveCheckpoint(context.Background(), connect.NewRequest(&runtimev1.SaveCheckpointRequest{
		CheckpointZipUploadUrl: "https://x",
	}))
	if err == nil {
		t.Fatal("expected error for missing project_name")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("expected CodeInvalidArgument, got %v: %v", connect.CodeOf(err), err)
	}
}

// signingTransport wraps a base RoundTripper and adds HMAC-V2 headers.
type signingTransport struct {
	secret []byte
	base   http.RoundTripper
}

func (s *signingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Read + restore body so we can sign over it
	var body []byte
	if req.Body != nil {
		buf := make([]byte, 0, 1024)
		tmp := make([]byte, 512)
		for {
			n, err := req.Body.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
			}
			if err != nil {
				break
			}
		}
		body = buf
		req.Body = httpBodyReadCloser(body)
		req.ContentLength = int64(len(body))
	}

	nonce := "nonce-test-" + time.Now().Format(time.RFC3339Nano)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := auth.ComputeSignatureBase64(s.secret, req.Method, req.URL.Path, nonce, ts, body)

	req.Header.Set(auth.HeaderSignature, sig)
	req.Header.Set(auth.HeaderNonce, nonce)
	req.Header.Set(auth.HeaderTimestamp, ts)

	base := s.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}
