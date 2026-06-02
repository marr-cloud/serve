# serve Go — Phase 3 Design: Brotli + HTTPS + Unix sockets + Windows named pipes

- **Owner:** meitrix8208@gmail.com
- **Date:** 2026-06-02
- **Project root:** `C:\Users\maurr\workspace\go\serve`
- **Predecessors:**
  - `2026-06-01-serve-go-design.md` (Phase 1, merged in `63033dd`)
  - `2026-06-02-serve-go-phase2-design.md` (Phase 2, merged in `424972f`)

## 1. Goal

Round out the CLI with the four advanced transport / compression features sketched in the Phase 1 design:

1. **Brotli** content encoding alongside gzip.
2. **HTTPS** via `--ssl-cert`, `--ssl-key`, `--ssl-pass` (npm `serve` parity).
3. **Unix domain sockets** (`unix:/path`) on Linux/macOS.
4. **Windows named pipes** (`pipe:\\.\pipe\serve`) on Windows.

After Phase 3 the binary matches npm `serve` on every documented production surface; only YAGNI features (hot-reload, Prometheus, proxy mode) remain outside the roadmap.

## 2. Non-goals

- HTTP/2, HTTP/3, QUIC.
- Self-signed cert autogeneration / Let's Encrypt / autocert.
- Brotli quality flag (fixed at level 5 — package default).
- File permissions or ACL configuration for unix sockets (fixed at `0660`).
- Named pipe security descriptor configuration (fixed at `Everyone` for the local-server use case).
- Benchmarks. F3 ships unit + integration + E2E test coverage only.
- TLS termination configuration beyond cert/key/pass (no SNI multi-cert, no cipher overrides, no ALPN tuning).

## 3. Architecture overview

```
internal/compress/
  compress.go        (modified)  Negotiate: prefers br > gzip
  gzip.go            (unchanged)
  brotli.go          (NEW)       NewBrotliEncoder

internal/listener/
  listener.go        (modified)  Build dispatches by scheme + applies tlsCfg
  listen_tcp.go      (NEW)       buildTCP extracted from listener.go
  listen_unix.go     (NEW)       //go:build unix       — buildUnix + cleanupListener
  listen_unix_stub.go(NEW)       //go:build !unix      — returns clear error
  listen_pipe_windows.go (NEW)   //go:build windows    — buildPipe via winio
  listen_pipe_stub.go    (NEW)   //go:build !windows   — returns clear error
  tls.go             (NEW)       LoadTLSConfig

internal/handler/handler.go      (modified) switch on Negotiate result
internal/config/{config.go,flags.go} (modified) +SSLCert/SSLKey/SSLPass

cmd/serve/main.go  (modified)  LoadTLSConfig → Build(addrs, switch, tlsCfg);
                               localAddr becomes https:// when applicable;
                               printHelp lists new flags
cmd/serve/serve_e2e_test.go (extended) +HTTPS, +unix/pipe (build-tagged)
```

## 4. Flags & config

Three new fields on `config.Config`, wired in `internal/config/flags.go`:

| Flag | Type | Default | Effect |
|---|---|---|---|
| `--ssl-cert <path>` | string | `""` | Enable TLS using this PEM cert. Requires `--ssl-key`. |
| `--ssl-key <path>` | string | `""` | PEM private key. Requires `--ssl-cert`. |
| `--ssl-pass <path>` | string | `""` | File containing the passphrase to decrypt an encrypted PEM key. Optional. |

CLI > serve.json precedence (Phase 2 contract): the cliSet map already covers these — they aren't valid `serve.json` keys, so the merge step ignores them. No changes to `MergeIntoConfig`.

## 5. Brotli

### 5.1 Dependency

`github.com/andybalholm/brotli` v1.x — pure Go (no cgo), MIT, actively maintained, only widely used Go Brotli implementation. Added to `go.mod`.

### 5.2 Encoder

`internal/compress/brotli.go`:

```go
package compress

import (
    "io"
    "github.com/andybalholm/brotli"
)

// NewBrotliEncoder returns an Encoder writing brotli-compressed bytes to w.
// Quality 5 is the default — best ratio/CPU trade-off for static assets per
// the upstream benchmarks (close to gzip 9 ratio at ~gzip 6 CPU cost).
func NewBrotliEncoder(w io.Writer) Encoder {
    return brotli.NewWriterLevel(w, 5)
}
```

`Encoder` is the existing `io.WriteCloser` alias from `gzip.go`. No interface changes required.

### 5.3 Negotiation

`internal/compress/compress.go`'s `Negotiate` is extended:

- Parses q-values as today.
- Recognizes `br` in addition to `gzip`.
- Preference: when both `br` and `gzip` are acceptable with equal q-values (the most common case), returns `"br"`.
- When q-values differ, the higher q wins; ties fall back to `br > gzip > identity`.
- Returns `"br"`, `"gzip"`, or `""` (identity).

### 5.4 Handler switch

`internal/handler/handler.go`'s `serveFile` switches on the Negotiate result:

```go
encoding := compress.Negotiate(r.Header.Get("Accept-Encoding"))
wantCompress := !c.cfg.NoCompression &&
    r.Header.Get("Range") == "" && // [BUG#1] preserved
    encoding != "" &&
    isCompressible(contentType, fsPath, info.Size())

if wantCompress {
    w.Header().Set("Content-Encoding", encoding)
    w.Header().Set("Vary", "Accept-Encoding")
    w.Header().Del("Content-Length")

    if r.Method == http.MethodHead { return }
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

`isCompressible` and `minCompressBytes` unchanged. The same Range/`isCompressible`/min-size gates apply equally to both encoders.

### 5.5 Tests

- `internal/compress/brotli_test.go`: write `[]byte("hello, brotli")` via `NewBrotliEncoder`, decompress with `brotli.NewReader`, assert byte-for-byte equality.
- `internal/compress/compress_test.go`: 5 new table rows
  - `"br"` → `"br"`
  - `"gzip"` → `"gzip"`
  - `"br, gzip"` → `"br"` (preference)
  - `"br;q=0.5, gzip;q=1.0"` → `"gzip"` (q-value wins)
  - `"identity"` → `""`
- `internal/handler/handler_test.go`: three new integration tests
  - `TestServe_BrotliForJS` — `Accept-Encoding: br` → `Content-Encoding: br`, roundtrip decode prefix matches.
  - `TestServe_PreferBrotliOverGzip` — `Accept-Encoding: gzip, br` → `Content-Encoding: br`.
  - `TestServe_RangeDisablesBrotli` — `Accept-Encoding: br` + `Range: bytes=0-49` → 206, no `Content-Encoding`.

## 6. HTTPS

### 6.1 `internal/listener/tls.go`

```go
// Package listener ... (existing).

// LoadTLSConfig constructs a *tls.Config from cert/key/passphrase paths.
// Returns (nil, nil) when certPath is empty (TLS disabled).
//
// When passphrasePath is non-empty, the key file is treated as an encrypted
// PEM (PKCS#1, "BEGIN RSA PRIVATE KEY" with a "Proc-Type" header) and
// decrypted with the passphrase read from that file (trailing whitespace
// trimmed). PKCS#8-encrypted keys ("BEGIN ENCRYPTED PRIVATE KEY") are NOT
// supported in F3 — Go stdlib has no decrypt path for them. Users on
// modern openssl can convert with:
//   openssl pkcs8 -in key.pem -traditional -out key.pkcs1.pem
func LoadTLSConfig(certPath, keyPath, passphrasePath string) (*tls.Config, error)
```

Validation rules:
- `certPath == ""` → returns `(nil, nil)` (TLS off; rest of flow unchanged).
- `certPath != "" && keyPath == ""` → returns error: `"--ssl-cert requires --ssl-key"`.
- `passphrasePath != ""`: read passphrase file, trim trailing `\r\n`, decode PEM, decrypt the block via `x509.DecryptPEMBlock` (deprecated-but-stdlib; the only stdlib path for encrypted PKCS#1 keys), re-encode as unencrypted PEM, pass to `tls.X509KeyPair`. Returns a clear error if the block type is `"ENCRYPTED PRIVATE KEY"` (PKCS#8 encrypted; user must convert first — see godoc on `LoadTLSConfig`).
- `passphrasePath == ""`: pass cert+key directly to `tls.X509KeyPair`.

Returned config:
```go
&tls.Config{
    Certificates: []tls.Certificate{cert},
    MinVersion:   tls.VersionTLS12,
}
```

### 6.2 `listener.Build` signature change

```go
// Build resolves each listen URI and binds it. When tlsCfg is non-nil,
// returned listeners are wrapped via tls.NewListener so the server speaks
// HTTPS over them. tlsCfg is NOT applied to pipe listeners (named pipes
// are a local-only transport where TLS adds no value).
func Build(addrs []string, allowPortSwitch bool, tlsCfg *tls.Config) ([]net.Listener, error)
```

The existing two-argument callers in `cmd/serve` get updated. There is one such caller.

### 6.3 Startup message

`cmd/serve/main.go` `localAddr` becomes `"https://localhost:" + port` when `tlsCfg != nil`. Network address line in `printStartupMessage` likewise prefixed.

### 6.4 Tests

`internal/listener/tls_test.go`:
- Helper `generateTestCert(t)` produces an in-memory self-signed cert + key (and optionally an encrypted variant) via `crypto/x509.CreateCertificate` + `crypto/ecdsa`. Avoids shipping test certs.
- `TestLoadTLSConfig_Plain` — plain cert/key → `*tls.Config` with `MinVersion == tls.VersionTLS12`.
- `TestLoadTLSConfig_Encrypted` — encrypted key + correct passphrase → loads OK.
- `TestLoadTLSConfig_WrongPassphrase` — returns error.
- `TestLoadTLSConfig_CertButNoKey` — returns the documented error message.
- `TestLoadTLSConfig_Empty` — `certPath == ""` returns `(nil, nil)`.
- `TestBuild_TLSWraps` — `Build([":0"], true, tlsCfg)`, dial with `tls.Dial` and a `InsecureSkipVerify` config, assert handshake completes.

## 7. Unix domain sockets

### 7.1 `internal/listener/listen_unix.go` (`//go:build unix`)

```go
//go:build unix

package listener

import (
    "net"
    "os"
)

// buildUnix binds a unix domain socket at addr. The socket file is removed
// if it exists (stale leftover from a previous run), then created with
// mode 0660 (owner+group rw). Returned listener removes the socket file
// on Close so subsequent runs see a clean state.
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

### 7.2 Stub (`listen_unix_stub.go`, `//go:build !unix`)

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

### 7.3 Tests (`listen_unix_test.go`, `//go:build unix`)

- Bind to `filepath.Join(t.TempDir(), "test.sock")`.
- Assert file exists after Listen.
- Assert `Stat().Mode().Perm() == 0o660` (skip on macOS if it doesn't honor `Chmod` post-bind — investigate during impl).
- Open an HTTP client via `net.Dial("unix", path)`, send `GET /`, expect a real response (use `http.Server{Handler: ...}.Serve(ln)` in a goroutine).
- Close the listener, assert socket file no longer exists.

## 8. Windows named pipes

### 8.1 Dependency

`github.com/Microsoft/go-winio` — official Microsoft package, BSD-3, no cgo. Implements `net.Listener` and `net.Conn` on Windows named pipes.

### 8.2 `listen_pipe_windows.go` (`//go:build windows`)

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

The SDDL `D:P(A;;GA;;;WD)` reads: discretionary ACL, protected, ACE = (allow, no flags, generic-all, no object-type, no inherit-object-type, Everyone).

### 8.3 Stub (`listen_pipe_stub.go`, `//go:build !windows`)

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

### 8.4 Tests (`listen_pipe_windows_test.go`, `//go:build windows`)

- Bind to `\\.\pipe\serve-test-<pid>` (PID makes the test idempotent across reruns / concurrent runs).
- Connect via `winio.DialPipe(addr, nil)`.
- Round-trip an HTTP request through `http.Server{Handler: ...}.Serve(ln)`.

## 9. Dispatcher (`listener.Build` body)

```go
func Build(addrs []string, allowPortSwitch bool, tlsCfg *tls.Config) ([]net.Listener, error) {
    out := make([]net.Listener, 0, len(addrs))
    for _, a := range addrs {
        scheme, addr, err := config.ParseListenURI(a)
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
```

`buildTCP` is the existing logic extracted to `listen_tcp.go` (currently inline in `listener.go`). The port-switching helper `NextAvailable` stays in `portswitch.go` unchanged.

**Note on `ParseListenURI` return type:** F1's `ParseListenURI` returns a canonicalized string. For F3 we expand it (or add a sibling `ParseListenURIScheme`) to also return the scheme. Backwards-compat: keep the existing `ParseListenURI` for any current caller and add `ParseListenURIScheme` that returns `(scheme, addr string, err error)`. Decision made at implementation time based on the call sites.

## 10. Wiring in `cmd/serve/main.go`

After Phase 2's flow (ParseFlags → rules.Load → MergeIntoConfig → dir resolution → SetExists), F3 inserts the TLS step before `listener.Build`:

```go
tlsCfg, err := listener.LoadTLSConfig(cfg.SSLCert, cfg.SSLKey, cfg.SSLPass)
if err != nil {
    log.Fatalf("tls: %v", err)
}

listeners, err := listener.Build(cfg.Listen, !cfg.NoPortSwitching, tlsCfg)
if err != nil {
    log.Fatalf("listener: %v", err)
}

// localAddr scheme depends on TLS.
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

For unix/pipe listeners, `net.SplitHostPort` returns an error (no host:port form). In that case `localAddr` stays empty and the startup banner shows only the bound addresses (already the behavior for non-TCP from F1).

`printHelp` documents the three new flags. Help text already exists for `-l/--listen unix:...` and `-l/--listen pipe:...` (F1 parser supported them; F3 actually binds them).

## 11. Testing summary

- **Unit:** brotli round-trip, compress.Negotiate br/gzip preference, LoadTLSConfig 5 cases, buildUnix bind+close (Unix), buildPipe bind+dial (Windows).
- **Integration in `internal/handler/handler_test.go`:** 3 new `TestServe_*` covering brotli + range guard.
- **E2E in `cmd/serve/serve_e2e_test.go`:**
  - `TestE2E_HTTPS` (all platforms) — generates self-signed cert in tempdir, `--ssl-cert/--ssl-key`, request via `tls.Dial` + `InsecureSkipVerify`.
  - `TestE2E_UnixSocket` (`//go:build unix`).
  - `TestE2E_NamedPipe` (`//go:build windows`).
- **CI matrix:** existing `ubuntu/macos/windows × Go 1.22` already exercises both build-tagged paths. No CI changes required.

## 12. Deliverables checklist (per Section 6 of the brainstorm)

1. `internal/compress/brotli.go` + `brotli_test.go`.
2. `internal/compress/compress.go` modified.
3. `internal/listener/tls.go` + `tls_test.go`.
4. `internal/listener/listen_tcp.go` (extract from `listener.go`).
5. `internal/listener/listen_unix.go` + `listen_unix_stub.go` + `listen_unix_test.go`.
6. `internal/listener/listen_pipe_windows.go` + `listen_pipe_stub.go` + `listen_pipe_windows_test.go`.
7. `internal/listener/listener.go` modified: `Build` accepts `tlsCfg`, dispatches by scheme.
8. `internal/handler/handler.go` modified: switch on Negotiate result.
9. `internal/config/{config.go,flags.go}` modified: `SSLCert`, `SSLKey`, `SSLPass`.
10. `cmd/serve/main.go` modified.
11. `cmd/serve/serve_e2e_test.go` extended.
12. `go.mod` / `go.sum`: `github.com/andybalholm/brotli`, `github.com/Microsoft/go-winio`.
13. `README.md`: HTTPS section, listener URI examples for `unix:`/`pipe:`, Brotli note.
14. `docs/superpowers/specs/2026-06-02-serve-go-phase3-design.md` (this file), `docs/superpowers/plans/2026-06-02-serve-go-phase3.md` (next step).

Unchanged: `internal/{rules,mime,logx}`, all Phase 2 fixtures.

## 13. Phase 3 — Done criteria

- [ ] `go vet ./...` clean
- [ ] `go test ./... -race -count=1` green on CI matrix (Linux / macOS / Windows)
- [ ] `./serve --ssl-cert <cert> --ssl-key <key> .` starts HTTPS; `curl -k https://localhost:3000/` returns 200
- [ ] `./serve -l unix:/tmp/serve.sock .` (Linux/macOS) serves; `curl --unix-socket /tmp/serve.sock http://x/` returns 200; socket file is removed after Ctrl-C
- [ ] `./serve -l pipe:\\.\pipe\serve .` (Windows) serves; a `winio.DialPipe` HTTP request returns 200
- [ ] `Accept-Encoding: br, gzip` request receives `Content-Encoding: br`
- [ ] Range request with either encoder requested still serves identity (BUG#1 preserved)
- [ ] Spec + plan + README committed; Phase 3 squash-merged to `main`; push; CI green.

## 14. Anticipated decisions / edge cases

- **TLS min version:** `tls.VersionTLS12` (no TLS 1.0/1.1; matches modern defaults).
- **No TLS on pipe listeners:** documented in `Build` doc comment.
- **Passphrase file format:** plain text, trailing whitespace trimmed. No support for prompt-on-stdin (CLI-only, no interactive).
- **Encrypted private keys: PKCS#1 only.** Go stdlib has no native decrypt for PKCS#8 encrypted keys (`BEGIN ENCRYPTED PRIVATE KEY`). Pulling a third-party dep just for that would be YAGNI for F3 — modern users typically have plaintext keys or can convert PKCS#8 → PKCS#1 with one `openssl pkcs8 -traditional` invocation. README documents the conversion. `x509.DecryptPEMBlock` is deprecated but is the only stdlib path; we accept the deprecation noise.
- **Brotli quality 5:** hardcoded. If a user benchmarks differently they can fork; no flag.
- **Unix socket file permissions 0660:** owner+group rw. Tighter than the default umask, looser than 0600 (so nginx running as the user's group can connect). Documented in README.
- **Named pipe SDDL `Everyone`:** appropriate for local static file server. Documented in README; if a use case ever appears for stricter ACL, add a flag later.
- **`go-winio` is only imported in `listen_pipe_windows.go`:** the `//go:build windows` constraint keeps it out of Linux/macOS builds. It still appears in `go.mod` but doesn't compile on non-Windows. CI confirms.
- **`ParseListenURI` signature:** decided at implementation time. Add a new `ParseListenURIScheme(s) (scheme, addr, err)` and keep the existing function as a thin wrapper that drops the scheme, or replace and update callers. Lean toward adding the new function to minimize blast radius.
