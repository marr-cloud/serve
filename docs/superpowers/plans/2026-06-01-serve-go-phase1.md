# serve (Go) — Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Refactor the monolithic `main.go` into a tested, modular CLI with bug fixes catalogued in the design spec, keeping observable behavior identical to today.

**Architecture:** A `cmd/serve` binary that wires together small leaf packages under `internal/`. Each package has one responsibility and is tested in isolation with `httptest`, `testing/fstest.MapFS`, and table tests. The new packages are built alongside the existing `main.go`; the final task swaps the entry point and deletes the old file.

**Tech Stack:** Go 1.22+, stdlib (`net/http`, `compress/gzip`, `io/fs`, `testing/fstest`, `httptest`), existing dependency `github.com/tiagomelo/go-clipboard`.

**Reference spec:** `docs/superpowers/specs/2026-06-01-serve-go-design.md`

---

## File Structure

After this plan, the repository looks like:

```
serve/
├── cmd/serve/
│   ├── main.go                  # wiring only, <50 LOC
│   └── serve_e2e_test.go        # smoke test of the wired binary
├── internal/
│   ├── config/
│   │   ├── config.go            # Config struct + defaults
│   │   ├── config_test.go
│   │   ├── flags.go             # flag.* parsing into Config
│   │   ├── flags_test.go
│   │   ├── listen.go            # ParseListenURI
│   │   └── listen_test.go
│   ├── handler/
│   │   ├── handler.go           # http.Handler entry point; middleware chain
│   │   ├── handler_test.go      # integration tests with fstest.MapFS
│   │   ├── files.go             # path resolve, stat, symlinks, SPA fallback
│   │   ├── files_test.go
│   │   ├── directory.go         # directory listing HTML
│   │   ├── directory_test.go
│   │   ├── etag.go              # generateETag
│   │   ├── etag_test.go
│   │   ├── headers.go           # CORS, Content-Type
│   │   └── headers_test.go
│   ├── compress/
│   │   ├── compress.go          # Negotiate + Encoder interface
│   │   ├── compress_test.go
│   │   ├── gzip.go              # gzip Encoder implementation
│   │   └── gzip_test.go
│   ├── listener/
│   │   ├── listener.go          # Build listeners from []string addrs
│   │   ├── listener_test.go
│   │   ├── portswitch.go        # try next port on EADDRINUSE
│   │   └── portswitch_test.go
│   ├── mime/
│   │   ├── mime.go              # deterministic table + DetectContentType fallback
│   │   └── mime_test.go
│   └── logx/
│       ├── logx.go              # request logger middleware
│       └── logx_test.go
├── docs/superpowers/
│   ├── specs/2026-06-01-serve-go-design.md
│   └── plans/2026-06-01-serve-go-phase1.md
├── .github/workflows/ci.yml     # vet + test + build matrix
├── .gitignore
├── README.md
├── go.mod                       # module serve, go 1.22
└── go.sum
```

The old top-level `main.go` is deleted in Task 17.

---

## Conventions used throughout

- **Module path:** stays `serve` (already in `go.mod`). Internal imports look like `serve/internal/config`.
- **Test discipline:** every task starts with a failing test (TDD). No code without a test that names the behavior.
- **Commit cadence:** one commit per task. Message format `<type>(<area>): <subject>` — e.g. `feat(config): add listen URI parser`. All commits include the Co-Authored-By trailer.
- **Bug fix tagging:** bugs from spec Section 3 are referenced as `[BUG#N]` in commit subjects so they're traceable.
- **Run tests after every step that adds code.** Don't move to the next task until tests are green.

---

### Task 1: Add Listen URI parser (TDD)

Implements [BUG#5] — robust parsing of listen addresses, including IPv6 and `tcp://` scheme, with clear errors and rejection of `unix:`/`pipe:` schemes (deferred to F3).

**Files:**
- Create: `internal/config/listen.go`
- Create: `internal/config/listen_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/config/listen_test.go`:

```go
package config

import (
	"strings"
	"testing"
)

func TestParseListenURI(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		{"bare port", "3000", "0.0.0.0:3000", ""},
		{"colon port", ":3000", "0.0.0.0:3000", ""},
		{"host and port", "localhost:3000", "localhost:3000", ""},
		{"tcp scheme", "tcp://localhost:3000", "localhost:3000", ""},
		{"ipv4 and port", "127.0.0.1:3000", "127.0.0.1:3000", ""},
		{"ipv6 and port", "[::1]:3000", "[::1]:3000", ""},
		{"port zero allowed", "0", "0.0.0.0:0", ""},
		{"empty", "", "", "empty"},
		{"non-numeric port", "host:abc", "", "invalid port"},
		{"port out of range high", "host:99999", "", "invalid port"},
		{"port negative", "host:-1", "", "invalid port"},
		{"unsupported scheme unix", "unix:/tmp/s.sock", "", "not supported"},
		{"unsupported scheme pipe", "pipe:\\\\.\\pipe\\s", "", "not supported"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseListenURI(tt.input)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tt.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/...`
Expected: FAIL with `undefined: ParseListenURI`.

- [ ] **Step 3: Implement `ParseListenURI`**

Create `internal/config/listen.go`:

```go
// Package config parses CLI flags and listen URIs into a typed configuration.
package config

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// ParseListenURI normalizes a user-supplied listen string into a canonical
// host:port suitable for net.Listen("tcp", …).
//
// Accepted forms:
//   - "3000"               → "0.0.0.0:3000"
//   - ":3000"              → "0.0.0.0:3000"
//   - "host:3000"          → "host:3000"
//   - "tcp://host:3000"    → "host:3000"
//   - "127.0.0.1:3000"     → "127.0.0.1:3000"
//   - "[::1]:3000"         → "[::1]:3000"
//
// "unix://" and "pipe://" return a "not supported" error (planned for F3).
func ParseListenURI(input string) (string, error) {
	if input == "" {
		return "", errors.New("empty listen address")
	}
	if idx := strings.Index(input, "://"); idx >= 0 {
		scheme := strings.ToLower(input[:idx])
		rest := input[idx+3:]
		switch scheme {
		case "tcp":
			input = rest
		case "unix", "pipe":
			return "", fmt.Errorf("scheme %q not supported in this version", scheme)
		default:
			return "", fmt.Errorf("scheme %q not supported", scheme)
		}
	}
	if strings.HasPrefix(input, "[") {
		host, port, err := net.SplitHostPort(input)
		if err != nil {
			return "", fmt.Errorf("invalid IPv6 address %q: %w", input, err)
		}
		if err := validatePort(port); err != nil {
			return "", err
		}
		return net.JoinHostPort(host, port), nil
	}
	if !strings.Contains(input, ":") {
		if err := validatePort(input); err != nil {
			return "", err
		}
		return "0.0.0.0:" + input, nil
	}
	if strings.HasPrefix(input, ":") {
		port := input[1:]
		if err := validatePort(port); err != nil {
			return "", err
		}
		return "0.0.0.0:" + port, nil
	}
	host, port, err := net.SplitHostPort(input)
	if err != nil {
		return "", fmt.Errorf("invalid listen address %q: %w", input, err)
	}
	if err := validatePort(port); err != nil {
		return "", err
	}
	return net.JoinHostPort(host, port), nil
}

func validatePort(p string) error {
	n, err := strconv.Atoi(p)
	if err != nil {
		return fmt.Errorf("invalid port %q", p)
	}
	if n < 0 || n > 65535 {
		return fmt.Errorf("invalid port %d (must be 0-65535)", n)
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/config/... -v`
Expected: all subtests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/listen.go internal/config/listen_test.go
git commit -m "feat(config): add ParseListenURI with IPv6 and scheme support [BUG#5]"
```

---

### Task 2: Add Config struct, defaults, and flag parsing (TDD)

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/flags.go`
- Create: `internal/config/flags_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/config/flags_test.go`:

```go
package config

import (
	"reflect"
	"testing"
)

func TestParseFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want Config
	}{
		{
			name: "defaults",
			args: []string{"serve"},
			want: Config{},
		},
		{
			name: "directory positional",
			args: []string{"serve", "./public"},
			want: Config{Directory: "./public"},
		},
		{
			name: "short single",
			args: []string{"serve", "-s"},
			want: Config{Single: true},
		},
		{
			name: "long single",
			args: []string{"serve", "--single"},
			want: Config{Single: true},
		},
		{
			name: "port short",
			args: []string{"serve", "-p", "8080"},
			want: Config{Port: 8080},
		},
		{
			name: "multiple listen",
			args: []string{"serve", "-l", "3000", "-l", "tcp://0.0.0.0:4000"},
			want: Config{Listen: []string{"3000", "tcp://0.0.0.0:4000"}},
		},
		{
			name: "cors and no-clipboard",
			args: []string{"serve", "-C", "-n"},
			want: Config{CORS: true, NoClipboard: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseFlags(tt.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/...`
Expected: FAIL with `undefined: ParseFlags` and `undefined: Config`.

- [ ] **Step 3: Implement Config struct**

Create `internal/config/config.go`:

```go
package config

// Version of the serve binary. Updated per release.
const Version = "0.1.0"

// Config holds all CLI options. Fields map 1-1 to the npm `serve` flags.
type Config struct {
	Directory        string
	Port             int
	Listen           []string
	Single           bool
	Debug            bool
	ConfigFile       string
	NoRequestLogging bool
	CORS             bool
	NoClipboard      bool
	NoCompression    bool
	NoETag           bool
	Symlinks         bool
	NoPortSwitching  bool
	Help             bool
	Version          bool
}
```

- [ ] **Step 4: Implement flag parsing**

Create `internal/config/flags.go`:

```go
package config

import (
	"flag"
	"fmt"
	"io"
)

type listenSlice []string

func (l *listenSlice) String() string     { return fmt.Sprintf("%v", []string(*l)) }
func (l *listenSlice) Set(v string) error { *l = append(*l, v); return nil }

// ParseFlags consumes args (including program name at index 0) and returns a
// populated Config. Errors are returned for unrecognized flags.
func ParseFlags(args []string) (Config, error) {
	var cfg Config
	var listen listenSlice

	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard) // we control help printing ourselves

	fs.Var(&listen, "l", "Specify a URI endpoint on which to listen")
	fs.Var(&listen, "listen", "Specify a URI endpoint on which to listen")
	fs.IntVar(&cfg.Port, "p", 0, "Specify custom port")
	fs.BoolVar(&cfg.Single, "s", false, "Rewrite all not-found requests to index.html")
	fs.BoolVar(&cfg.Single, "single", false, "Rewrite all not-found requests to index.html")
	fs.BoolVar(&cfg.Debug, "d", false, "Show debugging information")
	fs.BoolVar(&cfg.Debug, "debug", false, "Show debugging information")
	fs.StringVar(&cfg.ConfigFile, "c", "", "Specify custom path to serve.json")
	fs.StringVar(&cfg.ConfigFile, "config", "", "Specify custom path to serve.json")
	fs.BoolVar(&cfg.NoRequestLogging, "L", false, "Do not log any request information")
	fs.BoolVar(&cfg.NoRequestLogging, "no-request-logging", false, "Do not log any request information")
	fs.BoolVar(&cfg.CORS, "C", false, "Enable CORS, sets Access-Control-Allow-Origin to *")
	fs.BoolVar(&cfg.CORS, "cors", false, "Enable CORS, sets Access-Control-Allow-Origin to *")
	fs.BoolVar(&cfg.NoClipboard, "n", false, "Do not copy the local address to the clipboard")
	fs.BoolVar(&cfg.NoClipboard, "no-clipboard", false, "Do not copy the local address to the clipboard")
	fs.BoolVar(&cfg.NoCompression, "u", false, "Do not compress files")
	fs.BoolVar(&cfg.NoCompression, "no-compression", false, "Do not compress files")
	fs.BoolVar(&cfg.NoETag, "no-etag", false, "Send Last-Modified header instead of ETag")
	fs.BoolVar(&cfg.Symlinks, "S", false, "Resolve symlinks instead of showing 404 errors")
	fs.BoolVar(&cfg.Symlinks, "symlinks", false, "Resolve symlinks instead of showing 404 errors")
	fs.BoolVar(&cfg.NoPortSwitching, "no-port-switching", false, "Do not open a port other than the one specified when it's taken")
	fs.BoolVar(&cfg.Help, "help", false, "Shows this help message")
	fs.BoolVar(&cfg.Version, "v", false, "Displays the current version of serve")
	fs.BoolVar(&cfg.Version, "version", false, "Displays the current version of serve")

	if err := fs.Parse(args[1:]); err != nil {
		return Config{}, err
	}
	cfg.Listen = []string(listen)
	if positional := fs.Args(); len(positional) > 0 {
		cfg.Directory = positional[0]
	}
	return cfg, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/config/... -v`
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/config/config.go internal/config/flags.go internal/config/flags_test.go
git commit -m "feat(config): add Config struct and flag parser"
```

---

### Task 3: MIME table with deterministic extension lookup (TDD)

**Files:**
- Create: `internal/mime/mime.go`
- Create: `internal/mime/mime_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/mime/mime_test.go`:

```go
package mime

import "testing"

func TestTypeByExtension(t *testing.T) {
	tests := []struct {
		ext  string
		want string
	}{
		{".html", "text/html; charset=utf-8"},
		{".HTML", "text/html; charset=utf-8"}, // case-insensitive
		{".css", "text/css; charset=utf-8"},
		{".js", "application/javascript; charset=utf-8"},
		{".json", "application/json; charset=utf-8"},
		{".png", "image/png"},
		{".jpg", "image/jpeg"},
		{".svg", "image/svg+xml"},
		{".woff2", "font/woff2"},
		{".wasm", "application/wasm"},
		{".map", "application/json; charset=utf-8"},
		{".unknown_xyz", ""}, // signals "use sniffer"
	}
	for _, tt := range tests {
		t.Run(tt.ext, func(t *testing.T) {
			got := TypeByExtension(tt.ext)
			if got != tt.want {
				t.Fatalf("ext %q: got %q, want %q", tt.ext, got, tt.want)
			}
		})
	}
}

func TestIsAlreadyCompressed(t *testing.T) {
	cases := map[string]bool{
		".jpg":  true,
		".png":  true,
		".webp": true,
		".gz":   true,
		".br":   true,
		".woff2": true,
		".zip":  true,
		".mp4":  true,
		".html": false,
		".js":   false,
		".css":  false,
		".json": false,
		"":      false,
	}
	for ext, want := range cases {
		if got := IsAlreadyCompressed(ext); got != want {
			t.Errorf("ext %q: got %v, want %v", ext, got, want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/mime/...`
Expected: FAIL with `undefined: TypeByExtension` and `undefined: IsAlreadyCompressed`.

- [ ] **Step 3: Implement the MIME table**

Create `internal/mime/mime.go`:

```go
// Package mime maps file extensions to MIME types deterministically,
// without depending on /etc/mime.types or the Windows registry.
package mime

import (
	stdhttp "net/http"
	"os"
	"strings"
)

var table = map[string]string{
	".html":  "text/html; charset=utf-8",
	".htm":   "text/html; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".js":    "application/javascript; charset=utf-8",
	".mjs":   "application/javascript; charset=utf-8",
	".json":  "application/json; charset=utf-8",
	".map":   "application/json; charset=utf-8",
	".xml":   "application/xml; charset=utf-8",
	".txt":   "text/plain; charset=utf-8",
	".md":    "text/markdown; charset=utf-8",
	".csv":   "text/csv; charset=utf-8",
	".png":   "image/png",
	".jpg":   "image/jpeg",
	".jpeg":  "image/jpeg",
	".gif":   "image/gif",
	".webp":  "image/webp",
	".svg":   "image/svg+xml",
	".ico":   "image/x-icon",
	".pdf":   "application/pdf",
	".zip":   "application/zip",
	".gz":    "application/gzip",
	".br":    "application/octet-stream",
	".tar":   "application/x-tar",
	".wasm":  "application/wasm",
	".mp3":   "audio/mpeg",
	".mp4":   "video/mp4",
	".webm":  "video/webm",
	".woff":  "font/woff",
	".woff2": "font/woff2",
	".ttf":   "font/ttf",
	".otf":   "font/otf",
}

// TypeByExtension returns the MIME type for ext (case-insensitive, leading dot
// optional). Empty string means "unknown — use sniffer".
func TypeByExtension(ext string) string {
	if ext == "" {
		return ""
	}
	if ext[0] != '.' {
		ext = "." + ext
	}
	return table[strings.ToLower(ext)]
}

var compressedExts = map[string]struct{}{
	".jpg": {}, ".jpeg": {}, ".png": {}, ".gif": {}, ".webp": {},
	".gz": {}, ".br": {}, ".zip": {}, ".tar": {},
	".mp3": {}, ".mp4": {}, ".webm": {},
	".woff": {}, ".woff2": {},
}

// IsAlreadyCompressed reports whether content with this extension is typically
// already compressed (and therefore not worth re-compressing).
func IsAlreadyCompressed(ext string) bool {
	if ext == "" {
		return false
	}
	if ext[0] != '.' {
		ext = "." + ext
	}
	_, ok := compressedExts[strings.ToLower(ext)]
	return ok
}

// SniffContentType reads up to 512 bytes from path and uses net/http's
// content type sniffer, adding "; charset=utf-8" for text/ results.
// Returns "application/octet-stream" as ultimate fallback.
func SniffContentType(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return "application/octet-stream"
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	if n == 0 {
		return "application/octet-stream"
	}
	ct := stdhttp.DetectContentType(buf[:n])
	if strings.HasPrefix(ct, "text/") && !strings.Contains(ct, "charset") {
		ct += "; charset=utf-8"
	}
	return ct
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/mime/... -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/mime/mime.go internal/mime/mime_test.go
git commit -m "feat(mime): deterministic extension table + sniffer fallback"
```

---

### Task 4: ETag generator (TDD)

Implements [BUG#3] — removes md5 debug path. ETag is `"<modtime-unix-nanos-hex>-<size-hex>"` deterministically.

**Files:**
- Create: `internal/handler/etag.go`
- Create: `internal/handler/etag_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/handler/etag_test.go`:

```go
package handler

import (
	"testing"
	"testing/fstest"
	"time"
)

func TestGenerateETag(t *testing.T) {
	mod := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	a := &fstest.MapFile{Data: []byte("hello"), ModTime: mod}
	b := &fstest.MapFile{Data: []byte("hello"), ModTime: mod}
	if generateETag(infoFrom(a)) != generateETag(infoFrom(b)) {
		t.Fatal("same modtime+size should produce identical ETag")
	}

	c := &fstest.MapFile{Data: []byte("hello!"), ModTime: mod}
	if generateETag(infoFrom(a)) == generateETag(infoFrom(c)) {
		t.Fatal("different size should produce different ETag")
	}

	d := &fstest.MapFile{Data: []byte("hello"), ModTime: mod.Add(time.Second)}
	if generateETag(infoFrom(a)) == generateETag(infoFrom(d)) {
		t.Fatal("different modtime should produce different ETag")
	}
}

// infoFrom builds a fs.FileInfo from a MapFile via fstest.MapFS.
func infoFrom(mf *fstest.MapFile) fileInfo {
	return fileInfo{size: int64(len(mf.Data)), mod: mf.ModTime}
}

type fileInfo struct {
	size int64
	mod  time.Time
}

func (f fileInfo) Size() int64       { return f.size }
func (f fileInfo) ModTime() time.Time { return f.mod }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/handler/...`
Expected: FAIL with `undefined: generateETag`.

- [ ] **Step 3: Implement ETag**

Create `internal/handler/etag.go`:

```go
package handler

import (
	"fmt"
	"time"
)

// etagInfo is the minimal slice of fs.FileInfo that generateETag needs.
// Defined as an interface so tests can supply fakes without a full FileInfo.
type etagInfo interface {
	Size() int64
	ModTime() time.Time
}

// generateETag returns a strong ETag of the form `"<modtime-nanos>-<size>"`.
// Both components are hex. Deterministic given (modtime, size).
func generateETag(info etagInfo) string {
	return fmt.Sprintf(`"%x-%x"`, info.ModTime().UnixNano(), info.Size())
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/handler/... -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/handler/etag.go internal/handler/etag_test.go
git commit -m "feat(handler): deterministic ETag (modtime+size), drop md5 debug path [BUG#3]"
```

---

### Task 5: Content-Type + CORS headers (TDD)

**Files:**
- Create: `internal/handler/headers.go`
- Create: `internal/handler/headers_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/handler/headers_test.go`:

```go
package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestContentTypeFor(t *testing.T) {
	cases := map[string]string{
		"index.html":  "text/html; charset=utf-8",
		"app.js":      "application/javascript; charset=utf-8",
		"data.json":   "application/json; charset=utf-8",
		"logo.PNG":    "image/png", // case-insensitive
		"font.woff2":  "font/woff2",
	}
	for path, want := range cases {
		t.Run(path, func(t *testing.T) {
			if got := contentTypeFor(path, nil); got != want {
				t.Fatalf("path %q: got %q, want %q", path, got, want)
			}
		})
	}
}

func TestCORSMiddleware(t *testing.T) {
	handler := corsMiddleware(true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// OPTIONS request should short-circuit with 204.
	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS: status %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("OPTIONS: ACAO %q, want *", got)
	}

	// GET request: CORS headers present, body served.
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: status %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("GET: ACAO %q, want *", got)
	}
}

func TestCORSDisabled(t *testing.T) {
	handler := corsMiddleware(false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("CORS off: ACAO %q, want empty", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/handler/...`
Expected: FAIL with `undefined: contentTypeFor` and `undefined: corsMiddleware`.

- [ ] **Step 3: Implement headers and CORS middleware**

Create `internal/handler/headers.go`:

```go
package handler

import (
	"net/http"
	"path/filepath"

	"serve/internal/mime"
)

// contentTypeFor returns the Content-Type for a file path. If the extension
// is unknown, sniff is called (typically mime.SniffContentType) for fallback.
// Pass nil to skip sniffing and return "application/octet-stream".
func contentTypeFor(path string, sniff func(string) string) string {
	if ct := mime.TypeByExtension(filepath.Ext(path)); ct != "" {
		return ct
	}
	if sniff != nil {
		return sniff(path)
	}
	return "application/octet-stream"
}

// corsMiddleware optionally adds CORS headers. When enabled, OPTIONS
// requests return 204 with the headers set and short-circuit downstream
// handlers.
func corsMiddleware(enabled bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if !enabled {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "*")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/handler/... -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/handler/headers.go internal/handler/headers_test.go
git commit -m "feat(handler): Content-Type lookup + CORS middleware"
```

---

### Task 6: Gzip encoder + Accept-Encoding negotiation (TDD)

**Files:**
- Create: `internal/compress/compress.go`
- Create: `internal/compress/gzip.go`
- Create: `internal/compress/compress_test.go`
- Create: `internal/compress/gzip_test.go`

- [ ] **Step 1: Write the failing test for negotiation**

Create `internal/compress/compress_test.go`:

```go
package compress

import "testing"

func TestNegotiate(t *testing.T) {
	tests := []struct {
		name         string
		acceptHeader string
		want         string // "" = identity
	}{
		{"empty", "", ""},
		{"gzip only", "gzip", "gzip"},
		{"gzip with quality", "gzip;q=1.0", "gzip"},
		{"gzip and br", "br, gzip", "gzip"}, // br not yet supported in F1
		{"deflate only", "deflate", ""},      // not supported
		{"star", "*", "gzip"},
		{"gzip explicitly rejected", "gzip;q=0", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Negotiate(tt.acceptHeader); got != tt.want {
				t.Fatalf("Accept-Encoding %q: got %q, want %q", tt.acceptHeader, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Write the failing test for gzip encoder**

Create `internal/compress/gzip_test.go`:

```go
package compress

import (
	"bytes"
	stdgzip "compress/gzip"
	"io"
	"testing"
)

func TestGzipEncoder(t *testing.T) {
	input := bytes.Repeat([]byte("hello world\n"), 100)

	var buf bytes.Buffer
	enc := NewGzipEncoder(&buf)
	if _, err := enc.Write(input); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if buf.Len() >= len(input) {
		t.Fatalf("compressed size %d not smaller than input %d", buf.Len(), len(input))
	}

	gr, err := stdgzip.NewReader(&buf)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	got, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, input) {
		t.Fatal("roundtrip mismatch")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/compress/...`
Expected: FAIL with `undefined: Negotiate` and `undefined: NewGzipEncoder`.

- [ ] **Step 4: Implement negotiation**

Create `internal/compress/compress.go`:

```go
// Package compress negotiates and applies HTTP content encoding.
// F1 supports gzip and identity only; F3 adds brotli.
package compress

import (
	"io"
	"strings"
)

// Encoder wraps an io.Writer to apply content encoding.
type Encoder interface {
	io.WriteCloser
}

// Negotiate inspects an Accept-Encoding header and returns the chosen encoding.
// Returns "" (identity) when no supported encoding is acceptable.
//
// Algorithm: tokenize on ',', strip whitespace, examine the optional ";q=" weight.
// Accept gzip if it has a non-zero weight, or if "*" appears with non-zero weight.
func Negotiate(acceptEncoding string) string {
	if acceptEncoding == "" {
		return ""
	}
	gzipWeight, starWeight := -1.0, -1.0
	for _, part := range strings.Split(acceptEncoding, ",") {
		token, q := parseEncoding(strings.TrimSpace(part))
		switch token {
		case "gzip":
			gzipWeight = q
		case "*":
			starWeight = q
		}
	}
	if gzipWeight > 0 {
		return "gzip"
	}
	if gzipWeight < 0 && starWeight > 0 {
		return "gzip"
	}
	return ""
}

func parseEncoding(s string) (string, float64) {
	semi := strings.IndexByte(s, ';')
	if semi < 0 {
		return strings.ToLower(s), 1.0
	}
	token := strings.ToLower(strings.TrimSpace(s[:semi]))
	rest := s[semi+1:]
	if idx := strings.Index(rest, "q="); idx >= 0 {
		var q float64
		_, _ = fmtSscan(rest[idx+2:], &q)
		return token, q
	}
	return token, 1.0
}

// fmtSscan is a thin wrapper to keep Negotiate's imports minimal.
func fmtSscan(s string, q *float64) (int, error) {
	return sscanf(s, q)
}
```

Now add the `sscanf` helper. Append at the bottom of `compress.go`:

```go
import "strconv"

func sscanf(s string, q *float64) (int, error) {
	// Strip trailing semicolon-separated params.
	if idx := strings.IndexByte(s, ';'); idx >= 0 {
		s = s[:idx]
	}
	s = strings.TrimSpace(s)
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	*q = v
	return 1, nil
}
```

Wait — Go doesn't allow `import` after declarations. Replace the file with the consolidated version:

```go
package compress

import (
	"io"
	"strconv"
	"strings"
)

type Encoder interface {
	io.WriteCloser
}

func Negotiate(acceptEncoding string) string {
	if acceptEncoding == "" {
		return ""
	}
	gzipWeight, starWeight := -1.0, -1.0
	for _, part := range strings.Split(acceptEncoding, ",") {
		token, q := parseEncoding(strings.TrimSpace(part))
		switch token {
		case "gzip":
			gzipWeight = q
		case "*":
			starWeight = q
		}
	}
	if gzipWeight > 0 {
		return "gzip"
	}
	if gzipWeight < 0 && starWeight > 0 {
		return "gzip"
	}
	return ""
}

func parseEncoding(s string) (string, float64) {
	semi := strings.IndexByte(s, ';')
	if semi < 0 {
		return strings.ToLower(s), 1.0
	}
	token := strings.ToLower(strings.TrimSpace(s[:semi]))
	rest := s[semi+1:]
	if idx := strings.Index(rest, "q="); idx >= 0 {
		s := rest[idx+2:]
		if end := strings.IndexByte(s, ';'); end >= 0 {
			s = s[:end]
		}
		if v, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
			return token, v
		}
	}
	return token, 1.0
}
```

- [ ] **Step 5: Implement gzip encoder**

Create `internal/compress/gzip.go`:

```go
package compress

import (
	"compress/gzip"
	"io"
)

// NewGzipEncoder wraps w with a gzip writer at the default compression level.
// Close MUST be called to flush the gzip trailer.
func NewGzipEncoder(w io.Writer) Encoder {
	return gzip.NewWriter(w)
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/compress/... -v`
Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/compress/compress.go internal/compress/gzip.go internal/compress/compress_test.go internal/compress/gzip_test.go
git commit -m "feat(compress): Accept-Encoding negotiation + gzip encoder"
```

---

### Task 7: Path resolution, symlink policy, and traversal guard (TDD)

Implements [BUG#6] (path traversal) and [BUG#7] (symlink consistency).

**Files:**
- Create: `internal/handler/files.go`
- Create: `internal/handler/files_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/handler/files_test.go`:

```go
package handler

import (
	"runtime"
	"testing"
)

func TestResolvePath(t *testing.T) {
	root := "/var/www" // logical root for assertions; not touched on disk
	if runtime.GOOS == "windows" {
		root = `C:\www`
	}

	tests := []struct {
		name    string
		urlPath string
		want    string
		wantErr string
	}{
		{"root", "/", root, ""},
		{"file at root", "/index.html", root + sep() + "index.html", ""},
		{"nested", "/a/b/c.txt", root + sep() + "a" + sep() + "b" + sep() + "c.txt", ""},
		{"traversal dotdot", "/../etc/passwd", "", "traversal"},
		{"double slash", "//a", root + sep() + "a", ""},
		{"trailing slash kept", "/dir/", root + sep() + "dir", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolvePath(root, tt.urlPath)
			if tt.wantErr != "" {
				if err == nil || !contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func sep() string {
	if runtime.GOOS == "windows" {
		return `\`
	}
	return "/"
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && stringContains(s, sub)
}

func stringContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/handler/...`
Expected: FAIL with `undefined: resolvePath`.

- [ ] **Step 3: Implement path resolution**

Create `internal/handler/files.go`:

```go
package handler

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// resolvePath maps a URL path to an on-disk path under root, rejecting any
// request that would escape root via "..".
func resolvePath(root, urlPath string) (string, error) {
	// Strip query/fragment (callers should already do this, but defend).
	if i := strings.IndexAny(urlPath, "?#"); i >= 0 {
		urlPath = urlPath[:i]
	}
	clean := filepath.Clean("/" + strings.TrimPrefix(urlPath, "/"))
	full := filepath.Join(root, filepath.FromSlash(clean))
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("abs(root): %w", err)
	}
	absFull, err := filepath.Abs(full)
	if err != nil {
		return "", fmt.Errorf("abs(full): %w", err)
	}
	rel, err := filepath.Rel(absRoot, absFull)
	if err != nil {
		return "", fmt.Errorf("rel: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("path traversal rejected")
	}
	return full, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/handler/... -run TestResolvePath -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/handler/files.go internal/handler/files_test.go
git commit -m "feat(handler): resolvePath with traversal guard [BUG#6]"
```

---

### Task 8: SPA fallback decision (TDD)

Implements [BUG#4] — restrict the SPA fallback to HTML GET/HEAD requests without asset extensions.

**Files:**
- Modify: `internal/handler/files.go`
- Modify: `internal/handler/files_test.go`

- [ ] **Step 1: Add failing tests for shouldServeSPA**

Append to `internal/handler/files_test.go`:

```go
import "net/http"

func TestShouldServeSPA(t *testing.T) {
	mk := func(method, path, accept string) *http.Request {
		r := httptestNewRequest(method, path, accept)
		return r
	}
	cases := []struct {
		name string
		req  *http.Request
		want bool
	}{
		{"GET html accept", mk("GET", "/route", "text/html,*/*"), true},
		{"HEAD html accept", mk("HEAD", "/route", "text/html"), true},
		{"POST not allowed", mk("POST", "/route", "text/html"), false},
		{"JSON accept not allowed", mk("GET", "/api/x", "application/json"), false},
		{"asset extension blocks", mk("GET", "/app.js", "text/html"), false},
		{"png asset blocks", mk("GET", "/logo.png", "text/html"), false},
		{"no accept header", mk("GET", "/route", ""), false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldServeSPA(tt.req); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// httptestNewRequest is a tiny helper to avoid an extra import line above.
func httptestNewRequest(method, path, accept string) *http.Request {
	r, _ := http.NewRequest(method, path, nil)
	if accept != "" {
		r.Header.Set("Accept", accept)
	}
	return r
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/handler/... -run TestShouldServeSPA`
Expected: FAIL with `undefined: shouldServeSPA`.

- [ ] **Step 3: Add `net/http` to imports in files.go**

Replace the existing import block in `internal/handler/files.go` so it reads:

```go
import (
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
)
```

- [ ] **Step 4: Append shouldServeSPA to files.go**

Add to the end of `internal/handler/files.go`:

```go
// assetExtensions are file types we never serve through the SPA fallback.
// A request for one of these is treated as a missing asset, not a route.
var assetExtensions = map[string]struct{}{
	".js": {}, ".mjs": {}, ".css": {}, ".json": {}, ".map": {},
	".png": {}, ".jpg": {}, ".jpeg": {}, ".gif": {}, ".svg": {}, ".ico": {},
	".webp": {}, ".mp4": {}, ".mp3": {}, ".webm": {},
	".woff": {}, ".woff2": {}, ".ttf": {}, ".otf": {},
	".pdf": {}, ".zip": {}, ".gz": {}, ".tar": {}, ".wasm": {},
	".xml": {}, ".txt": {}, ".csv": {}, ".md": {},
}

// shouldServeSPA reports whether a missing file should be replaced with
// index.html. True iff method is GET/HEAD, Accept includes text/html, and
// the URL path doesn't have a known asset extension.
func shouldServeSPA(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	if !strings.Contains(r.Header.Get("Accept"), "text/html") {
		return false
	}
	ext := strings.ToLower(filepath.Ext(r.URL.Path))
	if _, isAsset := assetExtensions[ext]; isAsset {
		return false
	}
	return true
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/handler/... -v`
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/handler/files.go internal/handler/files_test.go
git commit -m "feat(handler): restrict SPA fallback to HTML GET/HEAD [BUG#4]"
```

---

### Task 9: Directory listing with HTML escape, sort, sizes (TDD)

Implements [BUG#8] (XSS via escape) and the cosmetic improvements (sort, size, modtime, no-cache headers).

**Files:**
- Create: `internal/handler/directory.go`
- Create: `internal/handler/directory_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/handler/directory_test.go`:

```go
package handler

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestServeDirectory_EscapesAndSorts(t *testing.T) {
	mod := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	fsys := fstest.MapFS{
		"sub/zeta":              &fstest.MapFile{ModTime: mod, Mode: 0o755},
		"sub/alpha.txt":         &fstest.MapFile{Data: []byte("a"), ModTime: mod},
		"sub/<script>.txt":      &fstest.MapFile{Data: []byte("x"), ModTime: mod},
		"sub/folder/.keep":      &fstest.MapFile{Data: []byte(""), ModTime: mod},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/sub/", nil)
	if err := serveDirectory(rec, req, fsys, "sub", "/sub/"); err != nil {
		t.Fatalf("serveDirectory: %v", err)
	}
	body, _ := io.ReadAll(rec.Body)
	html := string(body)

	if rec.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type: %q", rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("Cache-Control: %q", rec.Header().Get("Cache-Control"))
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("X-Content-Type-Options missing")
	}
	if strings.Contains(html, "<script>.txt") {
		t.Fatal("unescaped < in directory listing (XSS risk)")
	}
	if !strings.Contains(html, "&lt;script&gt;.txt") {
		t.Fatal("expected escaped < in directory listing")
	}
	idxFolder := strings.Index(html, "folder/")
	idxAlpha := strings.Index(html, "alpha.txt")
	if idxFolder < 0 || idxAlpha < 0 || idxFolder > idxAlpha {
		t.Fatal("expected folders listed before files, both present")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/handler/... -run TestServeDirectory`
Expected: FAIL with `undefined: serveDirectory`.

- [ ] **Step 3: Implement directory listing**

Create `internal/handler/directory.go`:

```go
package handler

import (
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strings"
)

// serveDirectory writes an HTML listing of entries in dirPath (relative to fsys)
// to w. urlPath is the request URL used to build links.
func serveDirectory(w http.ResponseWriter, r *http.Request, fsys fs.FS, dirPath, urlPath string) error {
	entries, err := fs.ReadDir(fsys, dirPath)
	if err != nil {
		return err
	}

	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir() // dirs first
		}
		return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
	})

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	var b strings.Builder
	fmt.Fprintf(&b, `<!DOCTYPE html><html><head><title>Index of %s</title>
<style>body{font-family:system-ui,sans-serif;margin:2rem;}
h1{border-bottom:1px solid #ccc;padding-bottom:.5rem;}
table{border-collapse:collapse;width:100%%;}
td{padding:.25rem .75rem;border-bottom:1px solid #eee;}
a{color:#06c;text-decoration:none;}a:hover{text-decoration:underline;}
.size,.mtime{color:#666;text-align:right;font-variant-numeric:tabular-nums;}</style>
</head><body><h1>Index of %s</h1><table>`,
		html.EscapeString(urlPath), html.EscapeString(urlPath))

	if urlPath != "/" {
		fmt.Fprint(&b, `<tr><td><a href="../">../</a></td><td></td><td></td></tr>`)
	}

	for _, e := range entries {
		name := e.Name()
		display := html.EscapeString(name)
		href := path.Join(urlPath, name)
		if e.IsDir() {
			display += "/"
			if !strings.HasSuffix(href, "/") {
				href += "/"
			}
		}
		// path.Join already URL-encodes nothing, so further encode is unnecessary
		// for ASCII names; for non-ASCII names we accept browser leniency.
		info, infoErr := e.Info()
		size, mtime := "", ""
		if infoErr == nil {
			if !e.IsDir() {
				size = formatSize(info.Size())
			}
			mtime = info.ModTime().UTC().Format("2006-01-02 15:04")
		}
		fmt.Fprintf(&b, `<tr><td><a href="%s">%s</a></td><td class="size">%s</td><td class="mtime">%s</td></tr>`,
			html.EscapeString(href), display, size, mtime)
	}

	fmt.Fprint(&b, `</table></body></html>`)
	_, err = w.Write([]byte(b.String()))
	return err
}

func formatSize(n int64) string {
	const k = 1024
	switch {
	case n < k:
		return fmt.Sprintf("%d B", n)
	case n < k*k:
		return fmt.Sprintf("%.1f KiB", float64(n)/k)
	case n < k*k*k:
		return fmt.Sprintf("%.1f MiB", float64(n)/(k*k))
	default:
		return fmt.Sprintf("%.1f GiB", float64(n)/(k*k*k))
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/handler/... -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/handler/directory.go internal/handler/directory_test.go
git commit -m "feat(handler): directory listing with HTML escape, sort, sizes [BUG#8]"
```

---

### Task 10: Main handler that orchestrates the middleware chain (TDD)

This is the largest task. It assembles every piece built so far into the public `http.Handler`, and applies fixes [BUG#1] (gzip+Range), [BUG#2] (Content-Length), and [BUG#7] (symlink consistency).

**Files:**
- Create: `internal/handler/handler.go`
- Create: `internal/handler/handler_test.go`

- [ ] **Step 1: Write the failing integration test**

Create `internal/handler/handler_test.go`:

```go
package handler

import (
	"bytes"
	stdgzip "compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"serve/internal/config"
)

func mkFS() fstest.MapFS {
	mod := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	return fstest.MapFS{
		"index.html":     &fstest.MapFile{Data: []byte("<html>root</html>"), ModTime: mod},
		"app.js":         &fstest.MapFile{Data: []byte(strings.Repeat("var x = 1;\n", 200)), ModTime: mod},
		"logo.png":       &fstest.MapFile{Data: []byte("\x89PNG\r\n\x1a\n" + strings.Repeat("x", 2000)), ModTime: mod},
		"sub/index.html": &fstest.MapFile{Data: []byte("<html>sub</html>"), ModTime: mod},
		"docs/readme":    &fstest.MapFile{Data: []byte("plain text content " + strings.Repeat("y", 2000)), ModTime: mod},
	}
}

func newHandler(cfg config.Config, fsys fstest.MapFS) http.Handler {
	return New(cfg, fsys)
}

func TestServe_File200(t *testing.T) {
	h := newHandler(config.Config{}, mkFS())
	req := httptest.NewRequest("GET", "/index.html", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("Content-Type %q", rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("ETag") == "" {
		t.Fatal("missing ETag")
	}
}

func TestServe_IfNoneMatch304(t *testing.T) {
	h := newHandler(config.Config{}, mkFS())
	// First request to discover ETag.
	r1 := httptest.NewRequest("GET", "/index.html", nil)
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, r1)
	etag := w1.Header().Get("ETag")
	if etag == "" {
		t.Fatal("expected ETag")
	}
	r2 := httptest.NewRequest("GET", "/index.html", nil)
	r2.Header.Set("If-None-Match", etag)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, r2)
	if w2.Code != http.StatusNotModified {
		t.Fatalf("expected 304, got %d", w2.Code)
	}
	if w2.Body.Len() != 0 {
		t.Fatal("304 must not have a body")
	}
}

func TestServe_GzipForJS(t *testing.T) {
	h := newHandler(config.Config{}, mkFS())
	req := httptest.NewRequest("GET", "/app.js", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("Content-Encoding %q", rec.Header().Get("Content-Encoding"))
	}
	if rec.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatalf("Vary %q", rec.Header().Get("Vary"))
	}
	// Verify roundtrip
	gr, err := stdgzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	got, _ := io.ReadAll(gr)
	if !strings.HasPrefix(string(got), "var x = 1;") {
		t.Fatal("decompressed content mismatch")
	}
}

func TestServe_NoGzipForPNG(t *testing.T) {
	h := newHandler(config.Config{}, mkFS())
	req := httptest.NewRequest("GET", "/logo.png", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatalf("PNG should not be gzipped, got %q", rec.Header().Get("Content-Encoding"))
	}
}

func TestServe_RangeDisablesGzip(t *testing.T) {
	h := newHandler(config.Config{}, mkFS())
	req := httptest.NewRequest("GET", "/app.js", nil)
	req.Header.Set("Accept-Encoding", "gzip")
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

func TestServe_DirectoryIndex(t *testing.T) {
	h := newHandler(config.Config{}, mkFS())
	req := httptest.NewRequest("GET", "/sub/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "sub") {
		t.Fatal("expected sub/index.html content")
	}
}

func TestServe_DirectoryListingWhenNoIndex(t *testing.T) {
	h := newHandler(config.Config{}, mkFS())
	req := httptest.NewRequest("GET", "/docs/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Index of") {
		t.Fatal("expected directory listing")
	}
}

func TestServe_SPA_HTMLAccept(t *testing.T) {
	h := newHandler(config.Config{Single: true}, mkFS())
	req := httptest.NewRequest("GET", "/some/route", nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "root") {
		t.Fatal("SPA should serve root index.html")
	}
}

func TestServe_SPA_JSONNoFallback(t *testing.T) {
	h := newHandler(config.Config{Single: true}, mkFS())
	req := httptest.NewRequest("GET", "/api/missing", nil)
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", rec.Code)
	}
}

func TestServe_PathTraversal(t *testing.T) {
	h := newHandler(config.Config{}, mkFS())
	req := httptest.NewRequest("GET", "/../etc/passwd", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound && rec.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403 or 404", rec.Code)
	}
}

func TestServe_OptionsWithCORS(t *testing.T) {
	h := newHandler(config.Config{CORS: true}, mkFS())
	req := httptest.NewRequest("OPTIONS", "/index.html", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d, want 204", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatal("expected ACAO *")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/handler/...`
Expected: FAIL with `undefined: New`.

- [ ] **Step 3: Implement the main handler**

Create `internal/handler/handler.go`:

```go
// Package handler is the HTTP handler that backs the serve CLI.
package handler

import (
	"bytes"
	"io"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"

	"serve/internal/compress"
	"serve/internal/config"
	"serve/internal/mime"
)

// New returns an http.Handler that serves files from fsys according to cfg.
func New(cfg config.Config, fsys fs.FS) http.Handler {
	core := &core{cfg: cfg, fsys: fsys}
	var h http.Handler = http.HandlerFunc(core.serve)
	h = corsMiddleware(cfg.CORS)(h)
	return h
}

type core struct {
	cfg  config.Config
	fsys fs.FS
}

func (c *core) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	// Resolve fs.FS path (forward slashes, no leading "/").
	urlPath := r.URL.Path
	if urlPath == "" {
		urlPath = "/"
	}
	cleaned := strings.TrimPrefix(filepath.ToSlash(filepath.Clean("/"+strings.TrimPrefix(urlPath, "/"))), "/")
	if cleaned == "" {
		cleaned = "."
	}
	if strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		http.NotFound(w, r)
		return
	}

	info, err := fs.Stat(c.fsys, cleaned)
	if err != nil {
		if c.cfg.Single && shouldServeSPA(r) {
			c.serveFile(w, r, "index.html")
			return
		}
		http.NotFound(w, r)
		return
	}

	// [BUG#7] symlink consistency: if symlinks are disabled and this entry is a
	// symlink, 404 before we open it via the FS (which would follow it).
	if !c.cfg.Symlinks {
		if lstat, ok := c.fsys.(interface {
			Lstat(string) (fs.FileInfo, error)
		}); ok {
			if li, lerr := lstat.Lstat(cleaned); lerr == nil && li.Mode()&fs.ModeSymlink != 0 {
				http.NotFound(w, r)
				return
			}
		}
	}

	if info.IsDir() {
		indexPath := pathJoin(cleaned, "index.html")
		if idx, err := fs.Stat(c.fsys, indexPath); err == nil && !idx.IsDir() {
			c.serveFile(w, r, indexPath)
			return
		}
		if err := serveDirectory(w, r, c.fsys, cleaned, ensureTrailingSlash(urlPath)); err != nil {
			http.Error(w, "Unable to read directory", http.StatusInternalServerError)
		}
		return
	}

	c.serveFile(w, r, cleaned)
}

func pathJoin(a, b string) string {
	if a == "." || a == "" {
		return b
	}
	return a + "/" + b
}

func ensureTrailingSlash(p string) string {
	if strings.HasSuffix(p, "/") {
		return p
	}
	return p + "/"
}

// serveFile is the single point where ETag, Content-Type, compression, and
// Range coexist. Gzip and Range are mutually exclusive: when both could apply,
// Range wins (compression is skipped). [BUG#1]
func (c *core) serveFile(w http.ResponseWriter, r *http.Request, fsPath string) {
	info, err := fs.Stat(c.fsys, fsPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	contentType := contentTypeFor(fsPath, func(string) string {
		return sniffViaFS(c.fsys, fsPath)
	})
	w.Header().Set("Content-Type", contentType)

	if c.cfg.NoETag {
		w.Header().Set("Last-Modified", info.ModTime().UTC().Format(http.TimeFormat))
	} else {
		etag := generateETag(etagAdapter{info})
		w.Header().Set("ETag", etag)
		if match := r.Header.Get("If-None-Match"); match == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	wantGzip := !c.cfg.NoCompression &&
		r.Header.Get("Range") == "" && // [BUG#1] never compress when Range is requested
		compress.Negotiate(r.Header.Get("Accept-Encoding")) == "gzip" &&
		isCompressible(contentType, fsPath, info.Size())

	if wantGzip {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Vary", "Accept-Encoding")
		// [BUG#2] Content-Length is unknown post-compression; let Go use chunked TE.
		w.Header().Del("Content-Length")

		if r.Method == http.MethodHead {
			return
		}
		f, err := c.fsys.Open(fsPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer f.Close()
		gz := compress.NewGzipEncoder(w)
		defer gz.Close()
		_, _ = io.Copy(gz, f)
		return
	}

	// Non-compressed path: use http.ServeContent so Range/If-Modified-Since work.
	f, err := c.fsys.Open(fsPath)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	var rs io.ReadSeeker
	if seeker, ok := f.(io.ReadSeeker); ok {
		rs = seeker
	} else {
		// Some fs.FS implementations don't expose Seek (e.g. real os.DirFS files
		// can, but custom FS may not). Buffer fully — acceptable for static assets.
		buf, ferr := io.ReadAll(f)
		if ferr != nil {
			http.Error(w, ferr.Error(), http.StatusInternalServerError)
			return
		}
		rs = bytes.NewReader(buf)
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), rs)
}

// etagAdapter adapts fs.FileInfo to the etagInfo interface in etag.go.
type etagAdapter struct{ fs.FileInfo }

func sniffViaFS(fsys fs.FS, p string) string {
	f, err := fsys.Open(p)
	if err != nil {
		return "application/octet-stream"
	}
	defer f.Close()
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	if n == 0 {
		return "application/octet-stream"
	}
	ct := http.DetectContentType(buf[:n])
	if strings.HasPrefix(ct, "text/") && !strings.Contains(ct, "charset") {
		ct += "; charset=utf-8"
	}
	return ct
}

const minCompressBytes = 1024

func isCompressible(contentType, path string, size int64) bool {
	if size < minCompressBytes {
		return false
	}
	if mime.IsAlreadyCompressed(filepath.Ext(path)) {
		return false
	}
	prefixes := []string{
		"text/",
		"application/javascript",
		"application/json",
		"application/xml",
		"application/xhtml+xml",
		"application/wasm",
		"image/svg+xml",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(contentType, p) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/handler/... -v`
Expected: all PASS (including all of the integration tests in Step 1).

- [ ] **Step 5: Commit**

```bash
git add internal/handler/handler.go internal/handler/handler_test.go
git commit -m "feat(handler): main http.Handler with gzip/Range/SPA fixes [BUG#1,#2,#7]"
```

---

### Task 11: Listener and port switching (TDD)

Implements [BUG#9] (real addr from listener) and [BUG#10] (named shutdown timeout constant).

**Files:**
- Create: `internal/listener/listener.go`
- Create: `internal/listener/listener_test.go`
- Create: `internal/listener/portswitch.go`
- Create: `internal/listener/portswitch_test.go`

- [ ] **Step 1: Write the failing test for portswitch**

Create `internal/listener/portswitch_test.go`:

```go
package listener

import (
	"net"
	"strings"
	"testing"
)

func TestNextAvailable(t *testing.T) {
	// Bind to port 0 to get an OS-assigned free port.
	lc, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("setup listen: %v", err)
	}
	defer lc.Close()

	taken := lc.Addr().String() // host:port
	got, err := NextAvailable(taken)
	if err != nil {
		t.Fatalf("NextAvailable: %v", err)
	}
	if got == taken {
		t.Fatal("should not return the in-use address")
	}
	if !strings.HasPrefix(got, "127.0.0.1:") {
		t.Fatalf("unexpected host: %q", got)
	}
}
```

- [ ] **Step 2: Write the failing test for Build**

Create `internal/listener/listener_test.go`:

```go
package listener

import (
	"strings"
	"testing"
)

func TestBuild_BindsEphemeralPort(t *testing.T) {
	lns, err := Build([]string{":0"}, true)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer func() {
		for _, l := range lns {
			l.Close()
		}
	}()
	if len(lns) != 1 {
		t.Fatalf("got %d listeners, want 1", len(lns))
	}
	addr := lns[0].Addr().String()
	if !strings.Contains(addr, ":") {
		t.Fatalf("unexpected addr %q", addr)
	}
}

func TestBuild_BadURIErrors(t *testing.T) {
	_, err := Build([]string{"unix:/tmp/x.sock"}, true)
	if err == nil {
		t.Fatal("expected error for unsupported scheme")
	}
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/listener/...`
Expected: FAIL with `undefined: Build`, `undefined: NextAvailable`.

- [ ] **Step 4: Implement Build and NextAvailable**

Create `internal/listener/listener.go`:

```go
// Package listener turns Config.Listen strings into net.Listener instances,
// optionally trying a successor port when the requested one is taken.
package listener

import (
	"fmt"
	"net"
	"time"

	"serve/internal/config"
)

// ShutdownTimeout is how long Server.Shutdown waits for in-flight requests. [BUG#10]
const ShutdownTimeout = 5 * time.Second

// Build resolves each input via config.ParseListenURI and binds it. If
// allowPortSwitch is true, an EADDRINUSE on bind triggers a search for the
// next free port (Section 11, npm-serve parity).
//
// Returned listeners are owned by the caller; close them on shutdown.
func Build(listenAddrs []string, allowPortSwitch bool) ([]net.Listener, error) {
	out := make([]net.Listener, 0, len(listenAddrs))
	for _, a := range listenAddrs {
		canon, err := config.ParseListenURI(a)
		if err != nil {
			closeAll(out)
			return nil, fmt.Errorf("parse %q: %w", a, err)
		}
		l, err := net.Listen("tcp", canon)
		if err != nil {
			if !allowPortSwitch {
				closeAll(out)
				return nil, fmt.Errorf("listen %q: %w", canon, err)
			}
			next, switchErr := NextAvailable(canon)
			if switchErr != nil {
				closeAll(out)
				return nil, fmt.Errorf("port switch from %q: %w", canon, switchErr)
			}
			l, err = net.Listen("tcp", next)
			if err != nil {
				closeAll(out)
				return nil, fmt.Errorf("listen %q after switch: %w", next, err)
			}
		}
		out = append(out, l)
	}
	return out, nil
}

func closeAll(lns []net.Listener) {
	for _, l := range lns {
		_ = l.Close()
	}
}
```

Create `internal/listener/portswitch.go`:

```go
package listener

import (
	"fmt"
	"net"
	"strconv"
)

// NextAvailable returns the first host:port after addr that successfully binds.
// Probes up to 100 successor ports; returns an error if none are available.
func NextAvailable(addr string) (string, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("split %q: %w", addr, err)
	}
	base, err := strconv.Atoi(portStr)
	if err != nil {
		return "", fmt.Errorf("port %q: %w", portStr, err)
	}
	for p := base + 1; p < base+1+100 && p < 65536; p++ {
		candidate := net.JoinHostPort(host, strconv.Itoa(p))
		ln, err := net.Listen("tcp", candidate)
		if err == nil {
			_ = ln.Close()
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no free port found near %q", addr)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/listener/... -v`
Expected: all PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/listener/listener.go internal/listener/listener_test.go internal/listener/portswitch.go internal/listener/portswitch_test.go
git commit -m "feat(listener): port switching + named ShutdownTimeout [BUG#9,#10]"
```

---

### Task 12: Request logger middleware (TDD)

**Files:**
- Create: `internal/logx/logx.go`
- Create: `internal/logx/logx_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/logx/logx_test.go`:

```go
package logx

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMiddleware_LogsAfterServe(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	h := Middleware(logger, false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("hello"))
	}))

	req := httptest.NewRequest("GET", "/x", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	out := buf.String()
	if !strings.Contains(out, "GET /x 418 5") {
		t.Fatalf("unexpected log line: %q", out)
	}
}

func TestMiddleware_Disabled(t *testing.T) {
	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)
	h := Middleware(logger, true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if buf.Len() != 0 {
		t.Fatalf("expected no logs when disabled, got %q", buf.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/logx/...`
Expected: FAIL with `undefined: Middleware`.

- [ ] **Step 3: Implement the middleware**

Create `internal/logx/logx.go`:

```go
// Package logx provides a tiny request logger middleware.
package logx

import (
	"log"
	"net/http"
	"time"
)

// Middleware logs `METHOD PATH STATUS BYTES DURATION` after each request.
// When disabled is true, the returned middleware is a passthrough.
func Middleware(logger *log.Logger, disabled bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if disabled {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &recorder{ResponseWriter: w, status: 200}
			next.ServeHTTP(rec, r)
			logger.Printf("%s %s %d %d %s",
				r.Method, r.URL.Path, rec.status, rec.bytes, time.Since(start))
		})
	}
}

type recorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *recorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/logx/... -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/logx/logx.go internal/logx/logx_test.go
git commit -m "feat(logx): request logger middleware"
```

---

### Task 13: Wire everything into `cmd/serve/main.go`

This task replaces the old top-level `main.go` with a thin entry point. It also implements [BUG#9] (real listener address in startup message).

**Files:**
- Create: `cmd/serve/main.go`
- Modify: nothing else yet (old `main.go` deleted in Task 14 after E2E green)

- [ ] **Step 1: Write the new main**

Create `cmd/serve/main.go`:

```go
// Command serve is a static file server compatible with the npm `serve` CLI.
package main

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/tiagomelo/go-clipboard/clipboard"

	"serve/internal/config"
	"serve/internal/handler"
	"serve/internal/listener"
	"serve/internal/logx"
)

func main() {
	cfg, err := config.ParseFlags(os.Args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if cfg.Help {
		printHelp()
		return
	}
	if cfg.Version {
		fmt.Printf("serve version %s\n", config.Version)
		return
	}
	if cfg.Directory == "" {
		wd, err := os.Getwd()
		if err != nil {
			log.Fatalf("getwd: %v", err)
		}
		cfg.Directory = wd
	}
	if _, err := os.Stat(cfg.Directory); err != nil {
		log.Fatalf("directory %q: %v", cfg.Directory, err)
	}
	if len(cfg.Listen) == 0 {
		if cfg.Port != 0 {
			cfg.Listen = []string{fmt.Sprintf("0.0.0.0:%d", cfg.Port)}
		} else {
			cfg.Listen = []string{"0.0.0.0:3000"}
		}
	}

	listeners, err := listener.Build(cfg.Listen, !cfg.NoPortSwitching)
	if err != nil {
		log.Fatalf("listener: %v", err)
	}

	h := logx.Middleware(log.Default(), cfg.NoRequestLogging)(
		handler.New(cfg, osDirFS(cfg.Directory)),
	)

	servers := make([]*http.Server, 0, len(listeners))
	for _, l := range listeners {
		srv := &http.Server{Handler: h}
		servers = append(servers, srv)
		go func(s *http.Server, l net.Listener) {
			if err := s.Serve(l); err != nil && err != http.ErrServerClosed {
				log.Printf("serve %s: %v", l.Addr(), err)
			}
		}(srv, l)
	}

	// [BUG#9] Compute the real local address from the first listener (post port-switch).
	localAddr := ""
	if len(listeners) > 0 {
		if _, port, splitErr := net.SplitHostPort(listeners[0].Addr().String()); splitErr == nil {
			localAddr = "http://localhost:" + port
		}
	}

	printStartupMessage(cfg, listeners, localAddr)

	if !cfg.NoClipboard && localAddr != "" {
		_ = clipboard.New().CopyText(localAddr)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	<-sigCh
	fmt.Println("\n INFO  Gracefully shutting down. Please wait...")

	ctx, cancel := context.WithTimeout(context.Background(), listener.ShutdownTimeout)
	defer cancel()
	for _, srv := range servers {
		_ = srv.Shutdown(ctx)
	}
}

// lstatFS wraps os.DirFS with Lstat so the handler can detect symlinks
// without following them. [BUG#7]
type lstatFS struct {
	root string
	fs   fs.FS
}

func osDirFS(root string) lstatFS {
	return lstatFS{root: root, fs: os.DirFS(root)}
}

func (l lstatFS) Open(name string) (fs.File, error) { return l.fs.Open(name) }

func (l lstatFS) Lstat(name string) (fs.FileInfo, error) {
	return os.Lstat(filepath.Join(l.root, filepath.FromSlash(name)))
}

func printStartupMessage(cfg config.Config, listeners []net.Listener, localAddr string) {
	fmt.Println("   ┌─────────────────────────────────────────────┐")
	fmt.Println("   │                                             │")
	fmt.Println("   │   Serving!                                  │")
	fmt.Println("   │                                             │")
	if localAddr != "" {
		fmt.Printf("   │   - Local:    %-30s│\n", localAddr)
	}
	for _, l := range listeners {
		addr := l.Addr().String()
		if strings.HasPrefix(addr, "0.0.0.0:") || strings.HasPrefix(addr, "[::]:") {
			if ip := outboundIP(); ip != "" {
				_, port, _ := net.SplitHostPort(addr)
				fmt.Printf("   │   - Network:  %-30s│\n", "http://"+ip+":"+port)
			}
		}
	}
	fmt.Println("   │                                             │")
	if !cfg.NoClipboard && localAddr != "" {
		fmt.Println("   │   Copied local address to clipboard!        │")
		fmt.Println("   │                                             │")
	}
	fmt.Println("   └─────────────────────────────────────────────┘")
}

func outboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return ""
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}

func printHelp() {
	fmt.Println(`  serve - Static file serving and directory listing

  USAGE
    $ serve --help
    $ serve --version
    $ serve folder_name
    $ serve [-l listen_uri [-l ...]] [directory]

    By default, serve will listen on 0.0.0.0:3000 and serve the
    current working directory on that address.

  OPTIONS
    --help                       Shows this help message
    -v, --version                Displays the current version of serve
    -l, --listen listen_uri      Specify a URI endpoint on which to listen
    -p                           Specify custom port
    -s, --single                 Rewrite all not-found requests to index.html
    -d, --debug                  Show debugging information
    -c, --config                 Specify custom path to serve.json (planned, F2)
    -L, --no-request-logging     Do not log any request information
    -C, --cors                   Enable CORS, sets Access-Control-Allow-Origin to *
    -n, --no-clipboard           Do not copy the local address to the clipboard
    -u, --no-compression         Do not compress files
    --no-etag                    Send Last-Modified header instead of ETag
    -S, --symlinks               Resolve symlinks instead of showing 404 errors
    --no-port-switching          Do not open a port other than the one specified when it's taken`)
}
```

- [ ] **Step 2: Verify it builds (old main.go still in place)**

Run: `go build ./cmd/serve`
Expected: builds without error. Both `cmd/serve` and the root `main.go` coexist temporarily (Go allows it because they're separate packages — top-level is `package main` for module root, and `cmd/serve/main.go` is `package main` for its own subdirectory).

Actually, with `module serve` and a top-level `main.go`, `go build ./...` would build both. We just verified `cmd/serve` builds. The next task removes the old file.

- [ ] **Step 3: Commit**

```bash
git add cmd/serve/main.go
git commit -m "feat(cmd): new cmd/serve entry point wiring all internal packages [BUG#9]"
```

---

### Task 14: E2E smoke test of the wired binary (TDD)

**Files:**
- Create: `cmd/serve/serve_e2e_test.go`

- [ ] **Step 1: Write the smoke test**

Create `cmd/serve/serve_e2e_test.go`:

```go
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

// TestE2E_GetHTML boots a server on an ephemeral port and exercises the full
// stack against a real temp directory.
func TestE2E_GetHTML(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>hi</h1>"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	lns, err := listener.Build([]string{":0"}, true)
	if err != nil {
		t.Fatalf("build listener: %v", err)
	}
	defer func() {
		for _, l := range lns {
			_ = l.Close()
		}
	}()

	cfg := config.Config{Directory: dir, NoRequestLogging: true}
	h := handler.New(cfg, osDirFS(dir))
	srv := &http.Server{Handler: h}
	go func() { _ = srv.Serve(lns[0]) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	addr := lns[0].Addr().String()
	if _, _, err := net.SplitHostPort(addr); err != nil {
		t.Fatalf("addr: %v", err)
	}

	resp, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "<h1>hi</h1>" {
		t.Fatalf("body: %q", body)
	}
}
```

- [ ] **Step 2: Run the test**

Run: `go test ./cmd/serve/...`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add cmd/serve/serve_e2e_test.go
git commit -m "test(cmd): E2E smoke test on ephemeral port"
```

---

### Task 15: Delete the old top-level `main.go`

This is the cutover. After this task, the only entry point is `cmd/serve/main.go`.

**Files:**
- Delete: `main.go` (at repo root)

- [ ] **Step 1: Verify the new binary builds and tests pass first**

Run: `go build ./cmd/serve && go test ./...`
Expected: build OK, all tests pass.

- [ ] **Step 2: Remove the old file**

Run:
```bash
git rm main.go
```

- [ ] **Step 3: Verify the build still works without the old file**

Run: `go build ./cmd/serve && go test ./...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git commit -m "refactor: remove monolithic main.go in favor of cmd/serve"
```

---

### Task 16: README documenting build, install, and flag reference

**Files:**
- Create: `README.md`

- [ ] **Step 1: Write the README**

Create `README.md`:

```markdown
# serve (Go)

A static file server that aims for behavioral parity with the npm `serve` CLI, implemented in Go for a faster runtime and a single self-contained binary.

## Status

Phase 1 (core CLI hardened). `serve.json` support is planned for Phase 2; Brotli, HTTPS, and Unix sockets are planned for Phase 3. See `docs/superpowers/specs/2026-06-01-serve-go-design.md` for the full roadmap.

## Install

```bash
go install serve/cmd/serve@latest
```

Or build from source:

```bash
git clone <repo>
cd serve
go build -o serve ./cmd/serve
```

## Usage

```bash
serve                  # serve the current directory on :3000
serve ./public         # serve ./public on :3000
serve -p 8080 ./dist   # custom port
serve -s ./dist        # SPA mode (HTML fallback)
serve -C ./public      # enable CORS
serve -l :4000 -l :4001 ./public  # multiple listen addresses
```

## Flags

| Flag | Description |
|------|-------------|
| `-l`, `--listen <addr>` | URI endpoint to listen on (may repeat). Accepts `port`, `:port`, `host:port`, `tcp://host:port`, `[::1]:port`. |
| `-p <port>` | Custom port (alias for `-l :port`) |
| `-s`, `--single` | Rewrite missing routes to `index.html` (SPA mode) |
| `-d`, `--debug` | Verbose debug output |
| `-c`, `--config <path>` | Path to `serve.json` (planned, Phase 2) |
| `-L`, `--no-request-logging` | Suppress per-request log lines |
| `-C`, `--cors` | Enable CORS (`Access-Control-Allow-Origin: *`) |
| `-n`, `--no-clipboard` | Don't copy local URL to clipboard |
| `-u`, `--no-compression` | Disable gzip compression |
| `--no-etag` | Send `Last-Modified` instead of `ETag` |
| `-S`, `--symlinks` | Resolve symlinks instead of returning 404 |
| `--no-port-switching` | Fail instead of trying successor ports when the requested port is taken |
| `-v`, `--version` | Print version and exit |
| `--help` | Print help and exit |

## Behavioral notes

- `gzip` and `Range` are mutually exclusive: a request with a `Range` header is served identity-encoded.
- SPA fallback (`-s`) only applies to GET/HEAD requests with `Accept: text/html` and a URL path without a known asset extension. Requests to `/api/*` or `*.json` return 404 normally.
- ETag is `"<modtime-unix-nanos>-<size>"`, deterministic across processes and machines.
- Directory listings are HTML-escaped (no XSS via filenames).

## Tests

```bash
go test ./...
```

## License

See LICENSE.
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: README with usage, flag reference, and behavior notes"
```

---

### Task 17: CI workflow on GitHub Actions

**Files:**
- Create: `.github/workflows/ci.yml`

- [ ] **Step 1: Write the workflow**

Create `.github/workflows/ci.yml`:

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  test:
    strategy:
      fail-fast: false
      matrix:
        os: [ubuntu-latest, macos-latest, windows-latest]
        go: ['1.22']
    runs-on: ${{ matrix.os }}
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: ${{ matrix.go }}
      - name: go vet
        run: go vet ./...
      - name: go test
        run: go test ./... -race -count=1
      - name: go build
        run: go build ./cmd/serve
```

- [ ] **Step 2: Commit**

```bash
mkdir -p .github/workflows
git add .github/workflows/ci.yml
git commit -m "ci: GitHub Actions vet+test+build matrix on Linux/macOS/Windows"
```

---

## Phase 1 — Done criteria

After Task 17, verify:

- [ ] `go vet ./...` clean
- [ ] `go test ./... -race -count=1` all green
- [ ] `go build ./cmd/serve` produces a working binary
- [ ] Running `./serve ./somewhere` behaves indistinguishably from the prior binary on the happy path (same flags, same startup message, same clipboard behavior)
- [ ] Each of the 10 bugs in spec Section 3 has a test that would fail under the old code
- [ ] `main.go` at the repo root no longer exists
- [ ] CI passes on Linux, macOS, and Windows
