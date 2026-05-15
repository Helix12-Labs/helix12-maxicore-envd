// Package auth implements Manus 1:1 HMAC-SHA256 V2 authentication for the
// MaxiCore in-VM Connect-RPC layer.
//
// Reverse-engineered from manus-sandbox v8.0.2 RAM dump 2026-05-14:
//   - Headers: X-Sandbox-Api-Signature-V2, X-Sandbox-Api-Nonce, X-Sandbox-Api-Timestamp
//   - sign-base: METHOD + "\n" + URL_PATH + "\n" + NONCE + "\n" + TIMESTAMP + "\n" + sha256(BODY)
//   - signature: base64(HMAC-SHA256(secret, sign-base))
//   - Two variants: RequireAuth (full timestamp+nonce+signature), RequireAuthSkipTimeCheck
//     (skip timestamp window for long-running ops e.g. SaveCheckpoint).
//
// Source scaffold: /home/xeno/projekte/Manus_Dump/05_neuer_dump_2026-05-14/
//
//	08_ram_dump/analysis/reverse_engineering/scaffold/auth_middleware.go
package auth

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Header names (RAM-verified from manus-sandbox v8.0.2).
const (
	HeaderSignature = "X-Sandbox-Api-Signature-V2" // V2 = HMAC-SHA256
	HeaderNonce     = "X-Sandbox-Api-Nonce"
	HeaderTimestamp = "X-Sandbox-Api-Timestamp"
)

// Error sentinels (RAM-verified strings).
var (
	ErrTimestampOutOfWindow     = errors.New("timestamp out of window")
	ErrInvalidSignatureEncoding = errors.New("invalid signature encoding")
	ErrSignatureMismatch        = errors.New("signature mismatch")
	ErrNonceReuse               = errors.New("nonce reuse detected")
	ErrMissingHeaders           = errors.New("missing required headers")
	ErrSecretUnavailable        = errors.New("sandbox secret unavailable (lazy: not yet provisioned)")
)

// Middleware implements HMAC-V2 auth with replay-protection via nonce-cache.
// Manus's auth.Middleware structure is mirrored 1:1.
type Middleware struct {
	secret     []byte
	secretFn   func() []byte // lazy provider; nil for static secret
	mu         sync.Mutex    // guards lazy secret resolution
	nonceCache *sync.Map     // map[nonce]time.Time
	timeWindow time.Duration
}

// NewMiddleware creates a new HMAC-V2 auth middleware.
//
// secret:     the per-sandbox HMAC secret (32 bytes recommended)
// timeWindow: max age of a request timestamp (default 60s in Manus)
//
// The middleware starts a background goroutine that reaps nonces older than
// 2x timeWindow. Call Stop() to terminate the reaper cleanly.
func NewMiddleware(secret []byte, timeWindow time.Duration) *Middleware {
	if timeWindow <= 0 {
		timeWindow = 60 * time.Second
	}
	m := &Middleware{
		secret:     secret,
		nonceCache: &sync.Map{},
		timeWindow: timeWindow,
	}
	go m.reapNonces()
	return m
}


// NewMiddlewareLazy creates a middleware that resolves the HMAC secret on
// first use via secretFn (result cached once non-empty). This lets the
// runtime.v1 surface be mounted BEFORE the secret is available — required
// because e2b TemplateCreate snapshots envd's running state during
// template-build (possibly before /etc/maxicore/sandbox-secret is readable)
// and resumes it later. A startup-only read would freeze envd in legacy-only
// mode forever; lazy resolution lets the rootfs file / e2b /init env take
// effect post-resume. Requests before the secret resolves return 401, not 404.
func NewMiddlewareLazy(secretFn func() []byte, timeWindow time.Duration) *Middleware {
	if timeWindow <= 0 {
		timeWindow = 60 * time.Second
	}
	m := &Middleware{
		secretFn:   secretFn,
		nonceCache: &sync.Map{},
		timeWindow: timeWindow,
	}
	go m.reapNonces()
	return m
}

// resolveSecret returns the HMAC secret, lazily invoking secretFn on first
// non-empty resolution and caching it. Returns nil if still unavailable.
func (m *Middleware) resolveSecret() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.secret) > 0 {
		return m.secret
	}
	if m.secretFn != nil {
		if sec := m.secretFn(); len(sec) > 0 {
			m.secret = sec
			return sec
		}
	}
	return nil
}

// RequireAuth wraps an http.Handler with full HMAC verification including
// timestamp-window check. Use for normal RPC endpoints.
func (m *Middleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := m.verify(r, true); err != nil {
			http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAuthSkipTimeCheck wraps an http.Handler with HMAC verification but
// without timestamp-window enforcement. Use for long-running operations
// (e.g. SaveCheckpoint, RestoreProject) that may take longer than the window.
// Replay-protection via nonce-cache still applies.
func (m *Middleware) RequireAuthSkipTimeCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := m.verify(r, false); err != nil {
			http.Error(w, "Unauthorized: "+err.Error(), http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// verify performs the full HMAC-SHA256 V2 signature check.
func (m *Middleware) verify(r *http.Request, checkTime bool) error {
	signature := r.Header.Get(HeaderSignature)
	nonce := r.Header.Get(HeaderNonce)
	timestamp := r.Header.Get(HeaderTimestamp)
	if signature == "" || nonce == "" || timestamp == "" {
		return ErrMissingHeaders
	}

	if checkTime {
		ts, err := strconv.ParseInt(timestamp, 10, 64)
		if err != nil {
			return ErrTimestampOutOfWindow
		}
		now := time.Now().Unix()
		if abs(now-ts) > int64(m.timeWindow.Seconds()) {
			return ErrTimestampOutOfWindow
		}
	}

	if err := m.verifyNonce(nonce); err != nil {
		return err
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return ErrSignatureMismatch
	}
	// reset body so downstream handlers can re-read
	r.Body = io.NopCloser(bytes.NewReader(body))

	secret := m.resolveSecret()
	if len(secret) == 0 {
		return ErrSecretUnavailable
	}
	expected := m.computeSignature(secret, r.Method, r.URL.Path, nonce, timestamp, body)
	provided, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return ErrInvalidSignatureEncoding
	}
	if !hmac.Equal(expected, provided) {
		return ErrSignatureMismatch
	}
	return nil
}

// verifyNonce checks the nonce-cache for replay-protection.
// First-seen nonces are stored with the current timestamp;
// subsequent requests with the same nonce are rejected.
func (m *Middleware) verifyNonce(nonce string) error {
	if _, loaded := m.nonceCache.LoadOrStore(nonce, time.Now()); loaded {
		return ErrNonceReuse
	}
	return nil
}

// computeSignature builds the HMAC-SHA256 over the canonical sign-base.
//
// sign-base = METHOD + "\n" + PATH + "\n" + NONCE + "\n" + TIMESTAMP + "\n" + sha256(BODY)
func (m *Middleware) computeSignature(secret []byte, method, path, nonce, timestamp string, body []byte) []byte {
	bodyHash := sha256.Sum256(body)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(method))
	mac.Write([]byte("\n"))
	mac.Write([]byte(path))
	mac.Write([]byte("\n"))
	mac.Write([]byte(nonce))
	mac.Write([]byte("\n"))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("\n"))
	mac.Write(bodyHash[:])
	return mac.Sum(nil)
}

// ComputeSignatureBase64 is a public helper for clients (e.g. the Backend signer)
// that need to generate a V2-signature for outgoing requests.
func ComputeSignatureBase64(secret []byte, method, path, nonce, timestamp string, body []byte) string {
	bodyHash := sha256.Sum256(body)
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(method))
	mac.Write([]byte("\n"))
	mac.Write([]byte(path))
	mac.Write([]byte("\n"))
	mac.Write([]byte(nonce))
	mac.Write([]byte("\n"))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("\n"))
	mac.Write(bodyHash[:])
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// reapNonces runs in the background and evicts nonces older than 2x timeWindow.
// Cache memory pressure is bounded by request-rate * 2 * timeWindow.
func (m *Middleware) reapNonces() {
	t := time.NewTicker(m.timeWindow)
	defer t.Stop()
	for range t.C {
		cutoff := time.Now().Add(-2 * m.timeWindow)
		m.nonceCache.Range(func(k, v any) bool {
			if ts, ok := v.(time.Time); ok && ts.Before(cutoff) {
				m.nonceCache.Delete(k)
			}
			return true
		})
	}
}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}
