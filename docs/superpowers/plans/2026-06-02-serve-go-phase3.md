# serve Go — Phase 3 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Brotli compression, HTTPS, Unix domain sockets, and Windows named pipes to the CLI.

**Architecture:** Brotli plugs into `internal/compress` alongside gzip with preference in `Negotiate`. HTTPS is a `*tls.Config` produced by `internal/listener.LoadTLSConfig` and applied by `listener.Build` via `tls.NewListener`. Alternative transports are build-tagged files in `internal/listener` (`listen_unix.go` for `//go:build unix`, `listen_pipe_windows.go` for `//go:build windows`, stubs for the other side). `listener.Build` dispatches by scheme.

**Tech Stack:** Go 1.22+, stdlib plus `github.com/andybalholm/brotli` and `github.com/Microsoft/go-winio`.

**Spec:** `docs/superpowers/specs/2026-06-02-serve-go-phase3-design.md`.

**Branch / worktree:** Use `superpowers:using-git-worktrees` to create branch `phase-3-transports` at worktree `.worktrees/phase-3-transports`.

---

## Task 1: Brotli encoder (`internal/compress/brotli.go`)

**Files:**
- Create: `internal/compress/brotli.go`
- Create: `internal/compress/brotli_test.go`
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Write the failing test**

Create `internal/compress/brotli_test.go`:

```go
package compress

import (
	"bytes"
	"io"
	"testing"

	"github.com/andybalholm/brotli"
)

func TestBrotliEncoder_RoundTrip(t *testing.T) {
	var buf bytes.Buffer
	enc := NewBrotliEncoder(&buf)
	want := []byte("hello, brotli — repeated " + repeatN("xyz", 200))
	if _, err := enc.Write(want); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if buf.Len() >= len(want) {
		t.Fatalf("compressed (%d) >= original (%d) — likely not compressed", buf.Len(), len(want))
	}
	got, err := io.ReadAll(brotli.NewReader(&buf))
	if err != nil {
		t.Fatalf("decompress: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("roundtrip mismatch:\nwant=%q\n got=%q", want, got)
	}
}

func repeatN(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
```

- [ ] **Step 2: Run the test and observe it fail**

Run: `go test ./internal/compress/... -run TestBrotliEncoder -v`
Expected: FAIL — `NewBrotliEncoder` undefined; `brotli` package not in go.mod.

- [ ] **Step 3: Add the dependency**

Run:
```bash
go get github.com/andybalholm/brotli@latest
```
Expected: `go.mod` and `go.sum` are updated.

- [ ] **Step 4: Implement `brotli.go`**

Create `internal/compress/brotli.go`:

```go
package compress

import (
	"io"

	"github.com/andybalholm/brotli"
)

// NewBrotliEncoder returns an Encoder writing brotli-compressed bytes to w.
// Quality 5 is the default: best ratio/CPU trade-off for static assets per
// the upstream benchmarks (close to gzip 9 ratio at roughly gzip 6 cost).
func NewBrotliEncoder(w io.Writer) Encoder {
	return brotli.NewWriterLevel(w, 5)
}
```

- [ ] **Step 5: Run the test and confirm it passes**

Run: `go test ./internal/compress/... -count=1 -v`
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/compress/brotli.go internal/compress/brotli_test.go go.mod go.sum
git commit -m "feat(compress): brotli encoder (quality 5)"
```

---

## Task 2: `compress.Negotiate` learns `br` with preference

**Files:**
- Modify: `internal/compress/compress.go`
- Modify: `internal/compress/compress_test.go`

- [ ] **Step 1: Extend the failing test table**

Open `internal/compress/compress_test.go`. Append these cases to the existing `TestNegotiate` table (do NOT remove existing rows):

```go
{name: "br only",              header: "br",                          want: "br"},
{name: "br preferred over gzip", header: "br, gzip",                  want: "br"},
{name: "q-value beats prefs",  header: "br;q=0.5, gzip;q=1.0",         want: "gzip"},
{name: "gzip ties with br at low q", header: "br;q=0.3, gzip;q=0.3",  want: "br"},
{name: "identity returns empty", header: "identity",                   want: ""},
```

- [ ] **Step 2: Run the tests and observe the new rows fail**

Run: `go test ./internal/compress/... -run TestNegotiate -v`
Expected: 5 new subtests FAIL — `Negotiate` only knows `gzip`.

- [ ] **Step 3: Update `Negotiate`**

Open `internal/compress/compress.go`. Replace the function body so it accepts both `br` and `gzip`, sorts by q-value descending, and breaks ties with `br > gzip > identity`:

```go
// Negotiate inspects an Accept-Encoding header value and returns the
// preferred encoding among "br", "gzip", and "" (identity).
// Preference: higher q-value wins; on ties, br > gzip > identity.
// Returns "" if no acceptable encoding is offered (or the client only
// accepts identity).
func Negotiate(acceptEncoding string) string {
	if acceptEncoding == "" {
		return ""
	}
	const (
		idxBr   = 0
		idxGzip = 1
	)
	q := [2]float64{-1, -1} // -1 means "not offered"
	for _, part := range strings.Split(acceptEncoding, ",") {
		name, qv := parseEncoding(strings.TrimSpace(part))
		switch name {
		case "br":
			if qv > q[idxBr] {
				q[idxBr] = qv
			}
		case "gzip":
			if qv > q[idxGzip] {
				q[idxGzip] = qv
			}
		}
	}
	switch {
	case q[idxBr] > q[idxGzip] && q[idxBr] > 0:
		return "br"
	case q[idxGzip] > q[idxBr] && q[idxGzip] > 0:
		return "gzip"
	case q[idxBr] > 0:
		return "br" // tie → br wins
	case q[idxGzip] > 0:
		return "gzip"
	default:
		return ""
	}
}
```

`parseEncoding` (already present) stays as-is. Imports stay as-is.

- [ ] **Step 4: Run the tests and confirm all rows pass**

Run: `go test ./internal/compress/... -run TestNegotiate -v`
Expected: every subtest PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/compress/compress.go internal/compress/compress_test.go
git commit -m "feat(compress): Negotiate learns br with preference over gzip"
```

---

## Task 3: Handler switches encoder by `Negotiate` result

**Files:**
- Modify: `internal/handler/handler.go`
- Modify: `internal/handler/handler_test.go`

- [ ] **Step 1: Write the failing integration tests**

Append to `internal/handler/handler_test.go`. Add a new import block entry for `github.com/andybalholm/brotli` alongside the existing imports:

```go
import (
	// existing imports …
	"github.com/andybalholm/brotli"
)

func TestServe_BrotliForJS(t *testing.T) {
	h := newHandler(config.Config{}, mkFS())
	req := httptest.NewRequest("GET", "/app.js", nil)
	req.Header.Set("Accept-Encoding", "br")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if rec.Header().Get("Content-Encoding") != "br" {
		t.Fatalf("Content-Encoding %q", rec.Header().Get("Content-Encoding"))
	}
	if rec.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatalf("Vary %q", rec.Header().Get("Vary"))
	}
	got, err := io.ReadAll(brotli.NewReader(rec.Body))
	if err != nil {
		t.Fatalf("brotli decode: %v", err)
	}
	if !strings.HasPrefix(string(got), "var x = 1;") {
		t.Fatalf("decoded prefix wrong: %q (Go 1.21+ builtin `min` is used)", string(got)[:min(20, len(got))])
	}
}

func TestServe_PreferBrotliOverGzip(t *testing.T) {
	h := newHandler(config.Config{}, mkFS())
	req := httptest.NewRequest("GET", "/app.js", nil)
	req.Header.Set("Accept-Encoding", "gzip, br")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Content-Encoding") != "br" {
		t.Fatalf("Content-Encoding %q, want br", rec.Header().Get("Content-Encoding"))
	}
}

func TestServe_RangeDisablesBrotli(t *testing.T) {
	h := newHandler(config.Config{}, mkFS())
	req := httptest.NewRequest("GET", "/app.js", nil)
	req.Header.Set("Accept-Encoding", "br")
	req.Header.Set("Range", "bytes=0-49")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status %d, want 206", rec.Code)
	}
	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatalf("Content-Encoding %q (must be empty when Range is set)", rec.Header().Get("Content-Encoding"))
	}
	if rec.Body.Len() != 50 {
		t.Fatalf("body length %d, want 50", rec.Body.Len())
	}
}
```

No helper needed: `min` is a Go 1.21+ builtin.

- [ ] **Step 2: Run the tests and observe they fail**

Run: `go test ./internal/handler/... -run TestServe_Brotli -v`
Expected: FAIL — handler still only emits gzip for `Accept-Encoding: br`.

- [ ] **Step 3: Replace the gzip branch with a switch in `serveFile`**

Open `internal/handler/handler.go`. Replace the existing `wantGzip` block with a generalized `wantCompress` block:

```go
encoding := compress.Negotiate(r.Header.Get("Accept-Encoding"))
wantCompress := !c.cfg.NoCompression &&
	r.Header.Get("Range") == "" && // [BUG#1] preserved
	encoding != "" &&
	isCompressible(contentType, fsPath, info.Size())

if wantCompress {
	w.Header().Set("Content-Encoding", encoding)
	w.Header().Set("Vary", "Accept-Encoding")
	w.Header().Del("Content-Length") // [BUG#2] unknown post-compression

	if r.Method == http.MethodHead {
		return
	}
	f, err := c.fsys.Open(fsPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()

	var enc compress.Encoder
	if encoding == "br" {
		enc = compress.NewBrotliEncoder(w)
	} else {
		enc = compress.NewGzipEncoder(w)
	}
	defer enc.Close()
	_, _ = io.Copy(enc, f)
	return
}
```

The remainder of `serveFile` (the `http.ServeContent` path) is unchanged.

- [ ] **Step 4: Run the tests and confirm everything passes**

Run: `go test ./internal/handler/... -count=1`
Expected: all PASS (the 3 new brotli tests and the existing gzip tests both green).

- [ ] **Step 5: Commit**

```bash
git add internal/handler/handler.go internal/handler/handler_test.go
git commit -m "feat(handler): serve brotli when Negotiate returns br"
```

---

## Task 4: Config flags for HTTPS

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/config/flags.go`
- Modify: `internal/config/flags_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/config/flags_test.go`:

```go
func TestParseFlags_SSLFlags(t *testing.T) {
	cfg, set, err := ParseFlags([]string{
		"serve",
		"--ssl-cert", "/etc/cert.pem",
		"--ssl-key", "/etc/key.pem",
		"--ssl-pass", "/etc/pass.txt",
	})
	if err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	if cfg.SSLCert != "/etc/cert.pem" || cfg.SSLKey != "/etc/key.pem" || cfg.SSLPass != "/etc/pass.txt" {
		t.Fatalf("cfg=%+v", cfg)
	}
	for _, name := range []string{"ssl-cert", "ssl-key", "ssl-pass"} {
		if !set[name] {
			t.Fatalf("expected %s in cliSet", name)
		}
	}
}
```

- [ ] **Step 2: Run the test and observe it fail**

Run: `go test ./internal/config/... -run TestParseFlags_SSLFlags -v`
Expected: FAIL — `SSLCert`/`SSLKey`/`SSLPass` not defined; flags not registered.

- [ ] **Step 3: Add the fields**

In `internal/config/config.go`, append three string fields to the `Config` struct (keep all other fields intact):

```go
type Config struct {
	// ... existing fields unchanged ...
	SSLCert string
	SSLKey  string
	SSLPass string
}
```

- [ ] **Step 4: Register the flags**

In `internal/config/flags.go`, add three `fs.StringVar` lines next to the other long-form flags (between `no-etag` and `S` registrations is a fine spot):

```go
fs.StringVar(&cfg.SSLCert, "ssl-cert", "", "PEM cert file for HTTPS (requires --ssl-key)")
fs.StringVar(&cfg.SSLKey, "ssl-key", "", "PEM key file for HTTPS (requires --ssl-cert)")
fs.StringVar(&cfg.SSLPass, "ssl-pass", "", "File containing passphrase for an encrypted PKCS#1 key")
```

- [ ] **Step 5: Run all config tests and confirm green**

Run: `go test ./internal/config/... -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/flags.go internal/config/flags_test.go
git commit -m "feat(config): --ssl-cert, --ssl-key, --ssl-pass flags"
```

---

## Task 5: `internal/listener/tls.go` — `LoadTLSConfig`

**Files:**
- Create: `internal/listener/tls.go`
- Create: `internal/listener/tls_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/listener/tls_test.go`:

```go
package listener

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writePlainCert generates a self-signed cert + unencrypted PKCS#1 PEM key
// into dir as cert.pem / key.pem and returns their paths.
func writePlainCert(t *testing.T, dir string) (certPath, keyPath string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "serve-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}
	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	keyDer, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDer}), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return certPath, keyPath
}

func TestLoadTLSConfig_Empty(t *testing.T) {
	cfg, err := LoadTLSConfig("", "", "")
	if err != nil {
		t.Fatalf("LoadTLSConfig: %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil cfg when certPath is empty")
	}
}

func TestLoadTLSConfig_CertButNoKey(t *testing.T) {
	if _, err := LoadTLSConfig("/nowhere/cert.pem", "", ""); err == nil {
		t.Fatal("expected error for --ssl-cert without --ssl-key")
	}
}

func TestLoadTLSConfig_Plain(t *testing.T) {
	cert, key := writePlainCert(t, t.TempDir())
	cfg, err := LoadTLSConfig(cert, key, "")
	if err != nil {
		t.Fatalf("LoadTLSConfig: %v", err)
	}
	if cfg == nil || len(cfg.Certificates) != 1 {
		t.Fatalf("expected 1 cert in config")
	}
	if cfg.MinVersion < tlsVersion12 {
		t.Fatalf("MinVersion %d, want >= TLS 1.2 (0x%x)", cfg.MinVersion, tlsVersion12)
	}
}
```

- [ ] **Step 2: Run the tests and observe they fail**

Run: `go test ./internal/listener/... -run TestLoadTLSConfig -v`
Expected: FAIL — `LoadTLSConfig`, `tlsVersion12` undefined.

- [ ] **Step 3: Implement `tls.go`**

Create `internal/listener/tls.go`:

```go
package listener

import (
	"crypto/tls"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
)

// tlsVersion12 is exposed for tests so they can assert MinVersion without
// importing crypto/tls themselves.
const tlsVersion12 = tls.VersionTLS12

// LoadTLSConfig constructs a *tls.Config from cert/key/passphrase paths.
// Returns (nil, nil) when certPath is empty (TLS disabled).
//
// When passphrasePath is non-empty, the key file is treated as an encrypted
// PEM (PKCS#1, "BEGIN RSA PRIVATE KEY" with a "Proc-Type" header) and
// decrypted with the passphrase read from that file (trailing whitespace
// trimmed). PKCS#8-encrypted keys ("BEGIN ENCRYPTED PRIVATE KEY") are NOT
// supported in this version — Go stdlib has no decrypt path for them.
// Users on modern openssl can convert with:
//
//	openssl pkcs8 -in key.pem -traditional -out key.pkcs1.pem
func LoadTLSConfig(certPath, keyPath, passphrasePath string) (*tls.Config, error) {
	if certPath == "" {
		return nil, nil
	}
	if keyPath == "" {
		return nil, fmt.Errorf("--ssl-cert requires --ssl-key")
	}
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("read cert %q: %w", certPath, err)
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read key %q: %w", keyPath, err)
	}
	if passphrasePath != "" {
		pass, err := os.ReadFile(passphrasePath)
		if err != nil {
			return nil, fmt.Errorf("read passphrase %q: %w", passphrasePath, err)
		}
		keyPEM, err = decryptPKCS1PEM(keyPEM, strings.TrimRight(string(pass), "\r\n \t"))
		if err != nil {
			return nil, err
		}
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("X509KeyPair: %w", err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// decryptPKCS1PEM decodes a PEM block, decrypts it with the passphrase using
// the legacy PKCS#1 mechanism, and re-encodes it as unencrypted PEM.
// Refuses PKCS#8-encrypted keys with a clear error pointing at the conversion
// recipe in the LoadTLSConfig godoc.
//
//nolint:staticcheck // x509.DecryptPEMBlock is the only stdlib decrypt path.
func decryptPKCS1PEM(keyPEM []byte, passphrase string) ([]byte, error) {
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return nil, fmt.Errorf("key PEM: no block found")
	}
	if block.Type == "ENCRYPTED PRIVATE KEY" {
		return nil, fmt.Errorf("PKCS#8 encrypted keys are not supported; convert with `openssl pkcs8 -in key.pem -traditional -out key.pkcs1.pem`")
	}
	der, err := decryptLegacy(block, []byte(passphrase))
	if err != nil {
		return nil, fmt.Errorf("decrypt key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: block.Type, Bytes: der}), nil
}
```

The actual call to `x509.DecryptPEMBlock` lives in a sibling file so the deprecation directive applies narrowly. Create `internal/listener/tls_decrypt.go`:

```go
package listener

import (
	"crypto/x509"
	"encoding/pem"
)

// decryptLegacy isolates the deprecated x509.DecryptPEMBlock call.
//
//nolint:staticcheck // x509.DecryptPEMBlock is the only stdlib decrypt path for PKCS#1.
func decryptLegacy(block *pem.Block, passphrase []byte) ([]byte, error) {
	return x509.DecryptPEMBlock(block, passphrase)
}
```

- [ ] **Step 4: Run the tests and confirm green**

Run: `go test ./internal/listener/... -run TestLoadTLSConfig -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/listener/tls.go internal/listener/tls_decrypt.go internal/listener/tls_test.go
git commit -m "feat(listener): LoadTLSConfig (cert+key, optional PKCS#1 passphrase)"
```

---

## Task 6: `ParseListenURIScheme` exposes the scheme

The current `ParseListenURI` returns only the canonical `host:port` string. F3's dispatcher needs the scheme separately. Add a sibling function that returns both; keep the existing function as a thin wrapper so other callers aren't disturbed.

**Files:**
- Modify: `internal/config/listen.go`
- Modify: `internal/config/listen_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/config/listen_test.go`:

```go
func TestParseListenURIScheme(t *testing.T) {
	cases := []struct {
		in         string
		wantScheme string
		wantAddr   string
		wantErr    bool
	}{
		{"3000", "tcp", "0.0.0.0:3000", false},
		{":3000", "tcp", "0.0.0.0:3000", false},
		{"127.0.0.1:3000", "tcp", "127.0.0.1:3000", false},
		{"tcp://0.0.0.0:8080", "tcp", "0.0.0.0:8080", false},
		{"unix:/tmp/serve.sock", "unix", "/tmp/serve.sock", false},
		{"unix:///tmp/serve.sock", "unix", "/tmp/serve.sock", false},
		{`pipe:\\.\pipe\serve`, "pipe", `\\.\pipe\serve`, false},
		{"garbage://x", "", "", true},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			scheme, addr, err := ParseListenURIScheme(c.in)
			if (err != nil) != c.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, c.wantErr)
			}
			if err != nil {
				return
			}
			if scheme != c.wantScheme || addr != c.wantAddr {
				t.Fatalf("scheme=%q addr=%q, want %q / %q", scheme, addr, c.wantScheme, c.wantAddr)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test and observe it fail**

Run: `go test ./internal/config/... -run TestParseListenURIScheme -v`
Expected: FAIL — `ParseListenURIScheme` undefined.

- [ ] **Step 3: Refactor `listen.go`**

Open `internal/config/listen.go`. Extract the scheme-detection logic so both functions share it. The simplest shape (without changing the existing `ParseListenURI` signature) is:

```go
// ParseListenURIScheme parses a listen URI and returns its scheme
// ("tcp" | "unix" | "pipe") and the transport-specific address.
// For unix, the address is a filesystem path. For pipe, it's the raw
// pipe path (e.g. `\\.\pipe\serve`). For tcp, it's `host:port`.
func ParseListenURIScheme(s string) (scheme, addr string, err error) {
	switch {
	case strings.HasPrefix(s, "unix://"):
		return "unix", strings.TrimPrefix(s, "unix://"), nil
	case strings.HasPrefix(s, "unix:"):
		return "unix", strings.TrimPrefix(s, "unix:"), nil
	case strings.HasPrefix(s, "pipe://"):
		return "pipe", strings.TrimPrefix(s, "pipe://"), nil
	case strings.HasPrefix(s, "pipe:"):
		return "pipe", strings.TrimPrefix(s, "pipe:"), nil
	}
	addr, err = ParseListenURI(s)
	if err != nil {
		return "", "", err
	}
	return "tcp", addr, nil
}
```

This relies on `ParseListenURI` continuing to reject the `unix:`/`pipe:` schemes when called directly (F1 behavior). `ParseListenURIScheme` short-circuits those cases first, so `ParseListenURI` is only called for TCP-style inputs.

- [ ] **Step 4: Run the tests and confirm green**

Run: `go test ./internal/config/... -count=1`
Expected: all PASS, including the new subtests.

- [ ] **Step 5: Commit**

```bash
git add internal/config/listen.go internal/config/listen_test.go
git commit -m "feat(config): ParseListenURIScheme returns (scheme, addr) for F3 dispatch"
```

---

## Task 7: Extract `buildTCP` and add `tlsCfg` parameter to `Build`

This task does NOT add unix/pipe support yet — it just reshapes `Build` so the dispatcher slot exists and TLS wraps the listeners. Tasks 8–9 fill the unix and pipe slots.

**Files:**
- Modify: `internal/listener/listener.go`
- Create: `internal/listener/listen_tcp.go`
- Modify: `internal/listener/listener_test.go`
- Modify: `cmd/serve/main.go` (caller)
- Modify: `cmd/serve/serve_e2e_test.go` (caller)

- [ ] **Step 1: Move existing TCP build code to `listen_tcp.go`**

Read `internal/listener/listener.go`. The existing `Build` body iterates inputs, calls `config.ParseListenURI`, then `net.Listen("tcp", canon)` with the port-switch fallback. Extract that per-address logic into a new file `internal/listener/listen_tcp.go`:

```go
package listener

import (
	"fmt"
	"net"
)

// buildTCP binds canon (a `host:port` string already validated by
// config.ParseListenURI). On EADDRINUSE, when allowPortSwitch is true,
// probes the next 100 ports via NextAvailable.
func buildTCP(canon string, allowPortSwitch bool) (net.Listener, error) {
	l, err := net.Listen("tcp", canon)
	if err == nil {
		return l, nil
	}
	if !allowPortSwitch {
		return nil, fmt.Errorf("listen %q: %w", canon, err)
	}
	next, switchErr := NextAvailable(canon)
	if switchErr != nil {
		return nil, fmt.Errorf("port switch from %q: %w", canon, switchErr)
	}
	l, err = net.Listen("tcp", next)
	if err != nil {
		return nil, fmt.Errorf("listen %q after switch: %w", next, err)
	}
	return l, nil
}
```

- [ ] **Step 2: Rewrite `Build` to dispatch by scheme and accept `tlsCfg`**

Replace `internal/listener/listener.go`'s `Build` with:

```go
// Package listener turns Config.Listen strings into net.Listener instances.
// Build dispatches by scheme (tcp/unix/pipe). When tlsCfg is non-nil, the
// returned TCP/unix listeners are wrapped via tls.NewListener; pipe
// listeners are not wrapped (local transport — TLS adds no value).
package listener

import (
	"crypto/tls"
	"fmt"
	"net"
	"time"

	"serve/internal/config"
)

// ShutdownTimeout is how long Server.Shutdown waits for in-flight requests. [BUG#10]
const ShutdownTimeout = 5 * time.Second

func Build(addrs []string, allowPortSwitch bool, tlsCfg *tls.Config) ([]net.Listener, error) {
	out := make([]net.Listener, 0, len(addrs))
	for _, a := range addrs {
		scheme, addr, err := config.ParseListenURIScheme(a)
		if err != nil {
			closeAll(out)
			return nil, fmt.Errorf("parse %q: %w", a, err)
		}
		var ln net.Listener
		switch scheme {
		case "tcp":
			ln, err = buildTCP(addr, allowPortSwitch)
		case "unix":
			ln, err = buildUnix(addr)
		case "pipe":
			ln, err = buildPipe(addr)
		default:
			err = fmt.Errorf("unsupported scheme %q", scheme)
		}
		if err != nil {
			closeAll(out)
			return nil, fmt.Errorf("listen %q: %w", a, err)
		}
		if tlsCfg != nil && scheme != "pipe" {
			ln = tls.NewListener(ln, tlsCfg)
		}
		out = append(out, ln)
	}
	return out, nil
}

func closeAll(lns []net.Listener) {
	for _, l := range lns {
		_ = l.Close()
	}
}
```

`buildUnix` and `buildPipe` are referenced here but defined in Tasks 8 and 9. To keep this task self-contained and compiling, also create the no-op stubs now. They will be replaced by real implementations in Tasks 8–9.

Create `internal/listener/listen_unix_stub.go`:

```go
//go:build !unix

package listener

import (
	"fmt"
	"net"
)

func buildUnix(addr string) (net.Listener, error) {
	return nil, fmt.Errorf("unix sockets require Linux/macOS")
}
```

Create a temporary unix-only placeholder so the package builds on Unix too. Create `internal/listener/listen_unix.go` (will be expanded in Task 8):

```go
//go:build unix

package listener

import (
	"fmt"
	"net"
)

func buildUnix(addr string) (net.Listener, error) {
	return nil, fmt.Errorf("unix sockets: not implemented yet (Task 8)")
}
```

Create `internal/listener/listen_pipe_stub.go`:

```go
//go:build !windows

package listener

import (
	"fmt"
	"net"
)

func buildPipe(addr string) (net.Listener, error) {
	return nil, fmt.Errorf("named pipes require Windows")
}
```

Create `internal/listener/listen_pipe_windows.go` (will be expanded in Task 9):

```go
//go:build windows

package listener

import (
	"fmt"
	"net"
)

func buildPipe(addr string) (net.Listener, error) {
	return nil, fmt.Errorf("named pipes: not implemented yet (Task 9)")
}
```

- [ ] **Step 3: Update existing test calls and callers**

Update `internal/listener/listener_test.go`. Add `nil` as the new third argument to every `Build(...)` call:

```go
lns, err := Build([]string{":0"}, true, nil)
// ...
_, err := Build([]string{"unix:/tmp/x.sock"}, true, nil)
```

Update `cmd/serve/main.go`'s call to `Build`. Pass `nil` for tlsCfg (TLS will be wired in Task 10):

```go
listeners, err := listener.Build(cfg.Listen, !cfg.NoPortSwitching, nil)
```

Update `cmd/serve/serve_e2e_test.go`'s call:

```go
lns, err := listener.Build([]string{":0"}, true, nil)
```

- [ ] **Step 4: Run all tests and build to confirm everything is green**

Run:
```
go vet ./...
go test ./... -count=1
go build ./cmd/serve
```
Expected: all packages green, binary builds.

- [ ] **Step 5: Commit**

```bash
git add internal/listener cmd/serve/main.go cmd/serve/serve_e2e_test.go
git commit -m "refactor(listener): Build dispatches by scheme + accepts *tls.Config (TCP-only behavior)"
```

---

## Task 8: Unix domain socket support

**Files:**
- Modify: `internal/listener/listen_unix.go` (replace placeholder with real impl)
- Create: `internal/listener/listen_unix_test.go`

- [ ] **Step 1: Write the failing test (Unix only)**

Create `internal/listener/listen_unix_test.go`:

```go
//go:build unix

package listener

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildUnix_BindsAndCleansUp(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "test.sock")
	ln, err := buildUnix(sock)
	if err != nil {
		t.Fatalf("buildUnix: %v", err)
	}
	if _, err := os.Stat(sock); err != nil {
		t.Fatalf("socket file missing after bind: %v", err)
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = conn.Close()
	if err := ln.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := os.Stat(sock); !os.IsNotExist(err) {
		t.Fatalf("socket file still present after Close: err=%v", err)
	}
}

func TestBuildUnix_RemovesStaleSocketFile(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "stale.sock")
	if err := os.WriteFile(sock, []byte{}, 0o600); err != nil {
		t.Fatalf("seed stale file: %v", err)
	}
	ln, err := buildUnix(sock)
	if err != nil {
		t.Fatalf("buildUnix: %v", err)
	}
	_ = ln.Close()
}
```

- [ ] **Step 2: Run the test and observe it fail (on Unix)**

Run (on Linux or macOS): `go test ./internal/listener/... -run TestBuildUnix -v`
Expected: FAIL — current placeholder returns `"not implemented yet"`.

- [ ] **Step 3: Replace the placeholder with the real implementation**

Replace `internal/listener/listen_unix.go` with:

```go
//go:build unix

package listener

import (
	"net"
	"os"
)

// buildUnix binds a unix domain socket at addr. Removes any stale socket
// file at that path first, then creates a fresh one with mode 0660.
// The returned listener removes the socket file on Close so subsequent
// runs see a clean state.
func buildUnix(addr string) (net.Listener, error) {
	_ = os.Remove(addr) // best-effort stale cleanup
	ln, err := net.Listen("unix", addr)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(addr, 0o660); err != nil {
		_ = ln.Close()
		return nil, err
	}
	return &cleanupListener{Listener: ln, path: addr}, nil
}

type cleanupListener struct {
	net.Listener
	path string
}

func (c *cleanupListener) Close() error {
	err := c.Listener.Close()
	_ = os.Remove(c.path)
	return err
}
```

- [ ] **Step 4: Run the tests and confirm they pass**

Run (on Linux or macOS): `go test ./internal/listener/... -count=1 -v -run TestBuildUnix`
Expected: both subtests PASS.

On Windows the file is excluded by build tag and `go test ./internal/listener/...` still passes (`buildUnix` resolves to the stub).

- [ ] **Step 5: Commit**

```bash
git add internal/listener/listen_unix.go internal/listener/listen_unix_test.go
git commit -m "feat(listener): unix domain sockets with stale cleanup + 0660 perms"
```

---

## Task 9: Windows named pipe support

**Files:**
- Modify: `internal/listener/listen_pipe_windows.go` (replace placeholder)
- Create: `internal/listener/listen_pipe_windows_test.go`
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add the winio dependency**

Run:
```bash
go get github.com/Microsoft/go-winio@latest
```
Expected: `go.mod` and `go.sum` updated. (On non-Windows hosts the dependency still resolves; it is only imported by Windows-tagged files, so non-Windows builds stay clean.)

- [ ] **Step 2: Write the failing test (Windows only)**

Create `internal/listener/listen_pipe_windows_test.go`:

```go
//go:build windows

package listener

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"

	winio "github.com/Microsoft/go-winio"
)

func TestBuildPipe_BindAndDial(t *testing.T) {
	addr := fmt.Sprintf(`\\.\pipe\serve-test-%d`, os.Getpid())
	ln, err := buildPipe(addr)
	if err != nil {
		t.Fatalf("buildPipe: %v", err)
	}
	defer ln.Close()

	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("pipe-ok"))
	})}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	conn, err := winio.DialPipe(addr, nil)
	if err != nil {
		t.Fatalf("DialPipe: %v", err)
	}
	defer conn.Close()
	if _, err := fmt.Fprint(conn, "GET / HTTP/1.0\r\n\r\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	resp, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytesContains(resp, []byte("pipe-ok")) {
		t.Fatalf("expected pipe-ok in response, got:\n%s", resp)
	}
}

func bytesContains(haystack, needle []byte) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if string(haystack[i:i+len(needle)]) == string(needle) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 3: Run the test on Windows and observe it fail**

Run (on Windows): `go test ./internal/listener/... -run TestBuildPipe -v`
Expected: FAIL — placeholder returns `"not implemented yet"`.

- [ ] **Step 4: Replace the placeholder**

Replace `internal/listener/listen_pipe_windows.go` with:

```go
//go:build windows

package listener

import (
	"net"

	winio "github.com/Microsoft/go-winio"
)

// buildPipe binds a Windows named pipe at addr (e.g. `\\.\pipe\serve`).
// The pipe is created with a permissive SDDL granting all access to
// "Everyone" — appropriate for a local static file server.
func buildPipe(addr string) (net.Listener, error) {
	cfg := &winio.PipeConfig{
		SecurityDescriptor: "D:P(A;;GA;;;WD)",
	}
	return winio.ListenPipe(addr, cfg)
}
```

- [ ] **Step 5: Run the test on Windows and confirm it passes**

Run (on Windows): `go test ./internal/listener/... -count=1 -v -run TestBuildPipe`
Expected: PASS.

On Linux/macOS the file is excluded by build tag and `go test ./internal/listener/...` still passes (`buildPipe` resolves to the stub).

- [ ] **Step 6: Commit**

```bash
git add internal/listener/listen_pipe_windows.go internal/listener/listen_pipe_windows_test.go go.mod go.sum
git commit -m "feat(listener): named pipes via Microsoft/go-winio (Everyone SDDL)"
```

---

## Task 10: Wire HTTPS through `cmd/serve/main.go`

**Files:**
- Modify: `cmd/serve/main.go`

- [ ] **Step 1: Build the tls config and pass it to `Build`; pick the right scheme for `localAddr`**

In `cmd/serve/main.go`, insert the TLS step right before `listener.Build` and rewrite the localAddr block. Replace the lines that compute `localAddr` and call `listener.Build`:

```go
tlsCfg, err := listener.LoadTLSConfig(cfg.SSLCert, cfg.SSLKey, cfg.SSLPass)
if err != nil {
	log.Fatalf("tls: %v", err)
}

listeners, err := listener.Build(cfg.Listen, !cfg.NoPortSwitching, tlsCfg)
if err != nil {
	log.Fatalf("listener: %v", err)
}

// [BUG#9] localAddr derives from the first listener's real Addr.
scheme := "http"
if tlsCfg != nil {
	scheme = "https"
}
localAddr := ""
if len(listeners) > 0 {
	if _, port, splitErr := net.SplitHostPort(listeners[0].Addr().String()); splitErr == nil {
		localAddr = scheme + "://localhost:" + port
	}
}
```

Keep the rest of `main.go` (signal handling, shutdown, banner, clipboard) unchanged.

- [ ] **Step 2: Update the help text**

In `printHelp`, append three lines to the `OPTIONS` block right before the closing backtick:

```
    --ssl-cert <path>            PEM cert file for HTTPS (requires --ssl-key)
    --ssl-key <path>             PEM key file for HTTPS (requires --ssl-cert)
    --ssl-pass <path>            File containing passphrase for encrypted PKCS#1 key
```

- [ ] **Step 3: Build and run the existing tests**

Run:
```
go vet ./...
go test ./... -count=1
go build ./cmd/serve
```
Expected: all green.

- [ ] **Step 4: Commit**

```bash
git add cmd/serve/main.go
git commit -m "feat(cmd): wire LoadTLSConfig + Build(tlsCfg); banner reflects https scheme"
```

---

## Task 11: E2E test for HTTPS (cross-platform)

**Files:**
- Modify: `cmd/serve/serve_e2e_test.go`

- [ ] **Step 1: Append the test**

In `cmd/serve/serve_e2e_test.go`, add a helper to generate an in-memory cert and a new test. Reuse the `writePlainCert` pattern from `internal/listener/tls_test.go` but inline it here (so the test stays self-contained):

```go
func TestE2E_HTTPS(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("secure-hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	certPath, keyPath := e2eWritePlainCert(t, dir)

	tlsCfg, err := listener.LoadTLSConfig(certPath, keyPath, "")
	if err != nil {
		t.Fatalf("LoadTLSConfig: %v", err)
	}
	lns, err := listener.Build([]string{":0"}, true, tlsCfg)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer func() {
		for _, l := range lns {
			_ = l.Close()
		}
	}()

	cfg := config.Config{Directory: dir, NoRequestLogging: true}
	h := handler.New(cfg, osDirFS(dir), nil)
	srv := &http.Server{Handler: h}
	go func() { _ = srv.Serve(lns[0]) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
	}}
	resp, err := client.Get("https://" + lns[0].Addr().String() + "/")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "secure-hi" {
		t.Fatalf("body: %q", body)
	}
}

func e2eWritePlainCert(t *testing.T, dir string) (certPath, keyPath string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "serve-e2e"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}
	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	keyDer, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDer}), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return certPath, keyPath
}
```

Add the necessary imports at the top of the file (merge with existing import block):

```go
import (
	// existing imports …
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
)
```

- [ ] **Step 2: Run the test and confirm it passes**

Run: `go test ./cmd/serve/... -count=1 -run TestE2E_HTTPS -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add cmd/serve/serve_e2e_test.go
git commit -m "test(cmd): E2E HTTPS via in-memory self-signed cert"
```

---

## Task 12: E2E test for unix socket (Unix only)

**Files:**
- Modify: `cmd/serve/serve_e2e_test.go` — split unix-only test into its own build-tagged file is cleaner. Create a new file instead.
- Create: `cmd/serve/serve_e2e_unix_test.go`

- [ ] **Step 1: Write the test**

Create `cmd/serve/serve_e2e_unix_test.go`:

```go
//go:build unix

package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"serve/internal/config"
	"serve/internal/handler"
	"serve/internal/listener"
)

func TestE2E_UnixSocket(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("unix-hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(t.TempDir(), "serve.sock")

	lns, err := listener.Build([]string{"unix:" + sock}, true, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer func() {
		for _, l := range lns {
			_ = l.Close()
		}
	}()

	cfg := config.Config{Directory: dir, NoRequestLogging: true}
	h := handler.New(cfg, osDirFS(dir), nil)
	srv := &http.Server{Handler: h}
	go func() { _ = srv.Serve(lns[0]) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", sock)
		},
	}}
	resp, err := client.Get("http://unix/")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "unix-hi" {
		t.Fatalf("body: %q", body)
	}
}
```

- [ ] **Step 2: Run on Linux/macOS and confirm**

Run (on Unix): `go test ./cmd/serve/... -count=1 -run TestE2E_UnixSocket -v`
Expected: PASS.

On Windows the file is excluded by build tag and the test does not run.

- [ ] **Step 3: Commit**

```bash
git add cmd/serve/serve_e2e_unix_test.go
git commit -m "test(cmd): E2E for unix socket transport (build-tagged)"
```

---

## Task 13: E2E test for Windows named pipe (Windows only)

**Files:**
- Create: `cmd/serve/serve_e2e_pipe_windows_test.go`

- [ ] **Step 1: Write the test**

Create `cmd/serve/serve_e2e_pipe_windows_test.go`:

```go
//go:build windows

package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	winio "github.com/Microsoft/go-winio"

	"serve/internal/config"
	"serve/internal/handler"
	"serve/internal/listener"
)

func TestE2E_NamedPipe(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("pipe-hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	addr := fmt.Sprintf(`\\.\pipe\serve-e2e-%d`, os.Getpid())

	lns, err := listener.Build([]string{"pipe:" + addr}, true, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer func() {
		for _, l := range lns {
			_ = l.Close()
		}
	}()

	cfg := config.Config{Directory: dir, NoRequestLogging: true}
	h := handler.New(cfg, osDirFS(dir), nil)
	srv := &http.Server{Handler: h}
	go func() { _ = srv.Serve(lns[0]) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return winio.DialPipeContext(ctx, addr)
		},
	}}
	resp, err := client.Get("http://pipe/")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "pipe-hi" {
		t.Fatalf("body: %q", body)
	}
	// silence the unused-import warning on platforms where filepath is only
	// used by the temp-dir helper above.
	_ = filepath.Join
}
```

- [ ] **Step 2: Run on Windows and confirm**

Run (on Windows): `go test ./cmd/serve/... -count=1 -run TestE2E_NamedPipe -v`
Expected: PASS.

On Linux/macOS the file is excluded and the test does not run.

- [ ] **Step 3: Commit**

```bash
git add cmd/serve/serve_e2e_pipe_windows_test.go
git commit -m "test(cmd): E2E for Windows named pipe transport (build-tagged)"
```

---

## Task 14: README updates

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Add HTTPS section and update listen URI / behavioral notes**

In `README.md`:

a) In the **Flags** table, replace the row referencing `serve.json` only with three new rows (insert after the existing `--no-port-switching` row):

```markdown
| `--ssl-cert <path>` | PEM cert file for HTTPS (requires `--ssl-key`) |
| `--ssl-key <path>` | PEM private key file for HTTPS |
| `--ssl-pass <path>` | File containing passphrase for an encrypted PKCS#1 key. PKCS#8-encrypted keys are not supported; convert with `openssl pkcs8 -in key.pem -traditional -out key.pkcs1.pem` |
```

b) Add a new top-level section right before `## serve.json`:

```markdown
## HTTPS

```bash
serve --ssl-cert ./cert.pem --ssl-key ./key.pem ./public
```

For an encrypted key, place the passphrase in a file (no trailing newline) and pass `--ssl-pass`. Only PKCS#1 PEM (`BEGIN RSA PRIVATE KEY` with a `Proc-Type` header) is supported. Modern openssl produces PKCS#8 by default; convert with `openssl pkcs8 -in key.pem -traditional -out key.pkcs1.pem`.

TLS minimum is 1.2. There's no support for autocert / Let's Encrypt — front with Caddy or nginx for that.

## Alternative listeners

Beyond TCP, the `-l` flag accepts:

```bash
serve -l unix:/tmp/serve.sock ./public         # Unix/macOS only
serve -l "pipe:\\.\pipe\serve" ./public        # Windows only
```

Unix sockets are created with mode `0660` and removed on shutdown. Named pipes use the SDDL `D:P(A;;GA;;;WD)` (allow `Everyone`), appropriate for a local-only file server.
```

c) In **Behavioral notes**, replace the line about gzip with:

```markdown
- Compression auto-detection: when the client sends `Accept-Encoding: br, gzip`, `serve` emits brotli (preference order `br > gzip > identity` on ties; explicit q-values still win).
- `Range` requests are served identity-encoded regardless of `Accept-Encoding`.
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: README for HTTPS, alternative listeners, brotli preference"
```

---

## Phase 3 — Done criteria

After Task 14, verify in the worktree:

- [ ] `go vet ./...` clean
- [ ] `go test ./... -count=1` all green locally; CI matrix passes `-race` on Linux/macOS/Windows
- [ ] `Accept-Encoding: br, gzip` to any compressible asset returns `Content-Encoding: br`; a Range request still skips compression
- [ ] `./serve --ssl-cert <c> --ssl-key <k> .` and `curl -k https://localhost:3000/` returns 200
- [ ] `./serve -l unix:/tmp/serve.sock .` and `curl --unix-socket /tmp/serve.sock http://x/` returns 200 (Unix)
- [ ] `./serve -l pipe:\\.\pipe\serve .` and a `winio.DialPipe` HTTP request returns 200 (Windows)
- [ ] Spec doc unchanged; plan doc (this file) committed in the worktree
- [ ] Branch ready to squash-merge into `main` via `superpowers:finishing-a-development-branch`
