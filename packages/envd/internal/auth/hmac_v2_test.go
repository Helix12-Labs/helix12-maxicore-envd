package auth

import (
	"bytes"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

const (
	testSecret = "test-secret-32-bytes-min-aaaaaaa"
	testPath   = "/runtime.v1.WebdevService/SaveCheckpoint"
)

func sign(t *testing.T, method, path, nonce, ts string, body []byte) string {
	t.Helper()
	return ComputeSignatureBase64([]byte(testSecret), method, path, nonce, ts, body)
}

func newSignedRequest(t *testing.T, method, path, nonce, ts string, body []byte) *http.Request {
	t.Helper()
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	r.Header.Set(HeaderSignature, sign(t, method, path, nonce, ts, body))
	r.Header.Set(HeaderNonce, nonce)
	r.Header.Set(HeaderTimestamp, ts)
	return r
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// drain body to verify downstream can still read it
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	})
}

// TC1: valid request → 200
func TestRequireAuth_ValidRequest_Accepted(t *testing.T) {
	mw := NewMiddleware([]byte(testSecret), 60*time.Second)
	body := []byte(`{"project_name":"test"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	r := newSignedRequest(t, http.MethodPost, testPath, "nonce-tc1", ts, body)

	w := httptest.NewRecorder()
	mw.RequireAuth(okHandler()).ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// TC2: replay-nonce → 401 "nonce reuse detected"
func TestRequireAuth_ReplayNonce_Rejected(t *testing.T) {
	mw := NewMiddleware([]byte(testSecret), 60*time.Second)
	body := []byte(`{}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := "nonce-tc2-shared"

	// First request — should succeed
	r1 := newSignedRequest(t, http.MethodPost, testPath, nonce, ts, body)
	w1 := httptest.NewRecorder()
	mw.RequireAuth(okHandler()).ServeHTTP(w1, r1)
	if w1.Code != http.StatusOK {
		t.Fatalf("first request expected 200, got %d", w1.Code)
	}

	// Second request with same nonce — should be rejected
	r2 := newSignedRequest(t, http.MethodPost, testPath, nonce, ts, body)
	w2 := httptest.NewRecorder()
	mw.RequireAuth(okHandler()).ServeHTTP(w2, r2)
	if w2.Code != http.StatusUnauthorized {
		t.Fatalf("replay expected 401, got %d", w2.Code)
	}
	if !bytes.Contains(w2.Body.Bytes(), []byte(ErrNonceReuse.Error())) {
		t.Fatalf("expected 'nonce reuse' in body, got: %s", w2.Body.String())
	}
}

// TC3: stale timestamp → 401 "timestamp out of window"
func TestRequireAuth_StaleTimestamp_Rejected(t *testing.T) {
	mw := NewMiddleware([]byte(testSecret), 30*time.Second)
	body := []byte(`{}`)
	staleTS := strconv.FormatInt(time.Now().Add(-5*time.Minute).Unix(), 10)
	r := newSignedRequest(t, http.MethodPost, testPath, "nonce-tc3", staleTS, body)

	w := httptest.NewRecorder()
	mw.RequireAuth(okHandler()).ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("stale ts expected 401, got %d", w.Code)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(ErrTimestampOutOfWindow.Error())) {
		t.Fatalf("expected 'timestamp out of window' in body, got: %s", w.Body.String())
	}
}

// TC4: tampered body → 401 "signature mismatch"
func TestRequireAuth_TamperedBody_Rejected(t *testing.T) {
	mw := NewMiddleware([]byte(testSecret), 60*time.Second)
	originalBody := []byte(`{"project_name":"original"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	// Sign over original body but send tampered body
	r := newSignedRequest(t, http.MethodPost, testPath, "nonce-tc4", ts, originalBody)
	r.Body = io.NopCloser(bytes.NewReader([]byte(`{"project_name":"tampered"}`)))

	w := httptest.NewRecorder()
	mw.RequireAuth(okHandler()).ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("tampered body expected 401, got %d", w.Code)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(ErrSignatureMismatch.Error())) {
		t.Fatalf("expected 'signature mismatch' in body, got: %s", w.Body.String())
	}
}

// TC5: missing headers → 401 "missing required headers"
func TestRequireAuth_MissingHeaders_Rejected(t *testing.T) {
	mw := NewMiddleware([]byte(testSecret), 60*time.Second)
	r := httptest.NewRequest(http.MethodPost, testPath, bytes.NewReader([]byte(`{}`)))
	// no headers

	w := httptest.NewRecorder()
	mw.RequireAuth(okHandler()).ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("missing headers expected 401, got %d", w.Code)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(ErrMissingHeaders.Error())) {
		t.Fatalf("expected 'missing required headers', got: %s", w.Body.String())
	}
}

// TC6: invalid base64 signature → 401 "invalid signature encoding"
func TestRequireAuth_InvalidSignatureEncoding_Rejected(t *testing.T) {
	mw := NewMiddleware([]byte(testSecret), 60*time.Second)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	r := httptest.NewRequest(http.MethodPost, testPath, bytes.NewReader([]byte(`{}`)))
	r.Header.Set(HeaderSignature, "not-base64!!!@@@")
	r.Header.Set(HeaderNonce, "nonce-tc6")
	r.Header.Set(HeaderTimestamp, ts)

	w := httptest.NewRecorder()
	mw.RequireAuth(okHandler()).ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("invalid base64 expected 401, got %d", w.Code)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(ErrInvalidSignatureEncoding.Error())) {
		t.Fatalf("expected 'invalid signature encoding', got: %s", w.Body.String())
	}
}

// TC7: RequireAuthSkipTimeCheck accepts stale timestamp
func TestRequireAuthSkipTimeCheck_StaleTimestamp_Accepted(t *testing.T) {
	mw := NewMiddleware([]byte(testSecret), 30*time.Second)
	body := []byte(`{}`)
	staleTS := strconv.FormatInt(time.Now().Add(-1*time.Hour).Unix(), 10)
	r := newSignedRequest(t, http.MethodPost, testPath, "nonce-tc7", staleTS, body)

	w := httptest.NewRecorder()
	mw.RequireAuthSkipTimeCheck(okHandler()).ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("skip-time-check expected 200 for stale ts, got %d: %s", w.Code, w.Body.String())
	}
}

// TC8: downstream handler can re-read body after middleware
func TestRequireAuth_BodyReadable_Downstream(t *testing.T) {
	mw := NewMiddleware([]byte(testSecret), 60*time.Second)
	body := []byte(`{"k":"v"}`)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	r := newSignedRequest(t, http.MethodPost, testPath, "nonce-tc8", ts, body)

	var captured []byte
	captureHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	})

	w := httptest.NewRecorder()
	mw.RequireAuth(captureHandler).ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !bytes.Equal(captured, body) {
		t.Fatalf("downstream body mismatch: got %q want %q", captured, body)
	}
}

// TC9: ComputeSignatureBase64 is deterministic
func TestComputeSignatureBase64_Deterministic(t *testing.T) {
	body := []byte(`{"x":1}`)
	a := ComputeSignatureBase64([]byte(testSecret), "POST", "/p", "n", "1000", body)
	b := ComputeSignatureBase64([]byte(testSecret), "POST", "/p", "n", "1000", body)
	if a != b {
		t.Fatalf("non-deterministic: %s != %s", a, b)
	}
	// also must be valid base64
	if _, err := base64.StdEncoding.DecodeString(a); err != nil {
		t.Fatalf("not valid base64: %v", err)
	}
}
