# serve Go — Phase 2 Design: `serve.json` Parity

- **Owner:** meitrix8208@gmail.com
- **Date:** 2026-06-02
- **Project root:** `C:\Users\maurr\workspace\go\serve`
- **Predecessor:** `2026-06-01-serve-go-design.md` (Phase 1, merged in `63033dd`)

## 1. Goal

Replicate the runtime semantics of [`serve.json`](https://github.com/vercel/serve-handler#options) in the Go reimplementation so that an arbitrary, valid `serve.json` from a real npm-`serve` project produces the same HTTP behavior under this binary.

**Closure criterion:** a Phase 2 build of `./serve -c serve.json` against a real-world npm-`serve` project (one not used as a fixture) yields identical status codes, redirect targets, response headers, and selected bodies for the project's documented routes. Verified manually with `curl`; no automated test for the spot check itself, but 4 golden fixtures lock down the regression surface.

## 2. Non-goals

- Hot-reload of `serve.json` without restart (deferred).
- Phase 3 work: Brotli, HTTPS, Unix sockets, Windows named pipes.
- Library / importable Go API. `serve` remains CLI-only.
- Benchmarks. F2 ships unit + integration + golden fixture coverage only.
- Wildcard authentication / access control. Outside the `serve.json` schema.

## 3. Architecture: hybrid Pre/Post middleware + internal queries

Rules are not uniform — they fan out across the request lifecycle:

| Stage | Rule keys | Mechanism |
|---|---|---|
| Pre-routing (URL mutation, early returns) | `redirects`, `rewrites`, `cleanUrls`, `trailingSlash` | `rules.Set.Pre()` middleware between `corsMiddleware` and `core.serve` |
| Handler decision (listing toggles) | `directoryListing`, `unlisted`, `renderSingle` | `core.serve` queries `*Set` directly |
| Post-response (header decoration) | `headers` | `rules.Set.Post()` wraps the `ResponseWriter` |
| Static config (load-time override) | `public`, `symlinks` | `cmd/serve` merges into `config.Config` before handler construction |

The handler signature changes to `handler.New(cfg, fsys, ruleSet)` (third argument may be `nil`).

## 4. Package layout

```
internal/rules/
  rules.go            # Set struct + Load() + parse + MergeIntoConfig
  rules_test.go
  match.go            # path-to-regexp v6 subset → *regexp.Regexp with named captures
  match_test.go
  pre.go              # Pre() middleware: redirects → rewrites → cleanUrls → trailingSlash
  pre_test.go
  post.go             # Post() middleware: headers
  post_test.go
  listing.go          # IsHidden, IsListingEnabled, RenderSingle queries
  listing_test.go
  testdata/           # 4 golden fixtures (each: serve.json + files/ + requests.json)
```

### 4.1 Public API surface

```go
// Set is the parsed, compiled view of a serve.json file.
type Set struct { /* unexported */ }

// Load resolves the config file in this order:
//   1. configFile if non-empty
//   2. <dir>/serve.json
//   3. <dir>/now.json   (legacy alias recognized by npm serve)
//   4. nothing → returns &Set{} (no-op)
// Parse errors are fatal: callers should log.Fatalf on err.
func Load(configFile, dir string) (*Set, error)

// MergeIntoConfig applies serve.json's `public` and `symlinks` onto cfg
// only when the user did NOT set the corresponding CLI flag. Precedence:
// CLI > serve.json > defaults.
func (s *Set) MergeIntoConfig(cfg *config.Config, cliSet map[string]bool)

// SetExists installs the filesystem-existence check used by cleanUrls.
// Called once by cmd/serve before constructing the handler; expected to
// resolve URL-relative paths against the same fs.FS the handler serves.
// Default (when not set) treats every file as non-existent — cleanUrls
// becomes a no-op, which is the safe degradation.
func (s *Set) SetExists(fn func(urlPath string) bool)

// Pre returns the middleware applied between CORS and the file-serving core.
// Handles redirects, rewrites, cleanUrls, trailingSlash.
func (s *Set) Pre() func(http.Handler) http.Handler

// Post returns the middleware that wraps ResponseWriter to inject matching
// header rules just before the body is written.
func (s *Set) Post() func(http.Handler) http.Handler

// Listing queries (called from internal/handler/directory.go and handler.go).
func (s *Set) IsHidden(urlPath string) bool
func (s *Set) IsListingEnabled(urlPath string) bool
func (s *Set) RenderSingle() bool
```

A `nil` `*Set` must be safe for all methods (each becomes a no-op or false return).

## 5. Pattern matcher (`match.go`)

Subset of [`path-to-regexp` v6](https://github.com/pillarjs/path-to-regexp/tree/v6.2.1) — the version used by `serve-handler`.

| Source syntax | Meaning | Compiled regex |
|---|---|---|
| `:name` | one segment, capture as group `name` | `(?P<name>[^/]+)` |
| `:name?` | optional segment | `(?:/(?P<name>[^/]+))?` (consumes the leading `/`) |
| `:name+` | one or more segments | `(?P<name>[^/].*?)` |
| `:name*` | zero or more segments | `(?P<name>.*)?` |
| `*` | wildcard, one segment, no capture | `[^/]+` |
| `**` | recursive wildcard, no capture | `.*` |
| `(regex)` | inline custom regex (rare in `serve.json`) | passthrough |
| literal | match exactly | `regexp.QuoteMeta(s)` |

```go
type Pattern struct {
    re   *regexp.Regexp
    keys []string // ordered capture names, "" for anonymous
}

func Compile(src string) (*Pattern, error)
func (p *Pattern) Match(urlPath string) (params map[string]string, ok bool)
func (p *Pattern) Expand(dest string, params map[string]string) string
```

`Expand` replaces `:name` in `dest` with the value of `params[name]`. Also supports `$1`, `$2`, ... for positional captures (`serve-handler` allows both). `dest` literals are emitted unchanged.

### 5.1 Test matrix (table-driven, `match_test.go`)

Minimum cases:

- `Compile("/api/:id")` matches `/api/42` → `{id:"42"}`; no match `/api/42/x`.
- `Compile("/files/**")` matches `/files/a/b/c`.
- `Compile("/old/:slug")` + `Expand("/new/:slug", ...)` produces `/new/<slug>`.
- Literal regex metachars in source (`.`, `+`) are escaped: `/v1.0/:x` does not treat `.` as wildcard.
- Edge cases: source `""`, source `"/"`, source containing `?` or `#` (must strip or reject consistently with the request normalization the handler already does).
- `Expand` with missing param: leaves the literal `:name` in place (parity with `serve-handler`'s behavior — does not error).

## 6. Rule semantics & order

### 6.1 Pre middleware

1. **`redirects`** — array of `{source, destination, type?}`. First match wins. Status defaults to `301`; `type` overrides to `301|302|307|308`. Destination is `Expand`ed with captured params.
2. **`rewrites`** — array of `{source, destination}`. First match wins. Mutates `r.URL.Path` to the expanded destination and continues the chain. **Single rewrite per request**: after one rewrite fires, the rewrite loop does not re-run (replicating `serve-handler`'s loop guard). Subsequent stages run on the rewritten URL.
3. **`cleanUrls`** — `bool | string[]`:
   - `true` or pattern-match: if URL has no extension, no trailing slash, and `<path>.html` exists in fs → rewrite to `<path>.html` internally; if URL has explicit `.html` → 301 to the version without `.html`.
   - Filesystem existence is checked via the `Exists` callback installed by `cmd/serve` through `Set.SetExists(fn)` (see Section 4.1). The callback closes over the handler's `fs.FS` and the resolved root. If no callback is installed, `cleanUrls` is a no-op (safe degradation; only relevant in tests that exercise `Pre` without wiring `cmd/serve`).
4. **`trailingSlash`** — `bool`:
   - `true`: enforce trailing slash on dir-like paths (301 add).
   - `false`: enforce no trailing slash (301 strip). **Note:** F1 already redirects `/dir` → `/dir/` for directories (handler.go BUG#2 fix). `trailingSlash: false` must override this — implemented by inserting a redirect-strip step in `Pre` that runs before the handler's own redirect; if the rule fires, the handler's redirect never sees the request.

### 6.2 Post middleware

5. **`headers`** — array of `{source, headers: [{key, value}]}`. **All matches apply, in order.** `value` may contain `:param` references from the matched capture; `Expand` runs on `value` as well as `key`. Rule-set headers override handler-set headers (handler writes first, Post wraps the writer and overrides on `WriteHeader`).

### 6.3 Listing queries

6. **`directoryListing`** — `bool | string[]`. Default `true`. If `false` or pattern doesn't match → return 404 from the dir-listing branch in `handler.serve`.
7. **`unlisted`** — `string[]`. Patterns of filenames (not full URL paths — match `entry.Name()` against the pattern with no leading `/`) to drop from listings. Applied in `serveDirectory` while filtering entries.
8. **`renderSingle`** — `bool`. When a directory contains exactly one non-`unlisted` file → serve that file directly instead of generating a listing.

### 6.4 Static config

9. **`public`** — string. Override of the root directory. `cmd/serve` resolves it (relative paths resolved against the original `cfg.Directory`) and reassigns `cfg.Directory`. CLI `-d <path>` wins.
10. **`symlinks`** — bool. Override of `cfg.Symlinks`. CLI `-S` wins.

## 7. Config & CLI integration

### 7.1 Detecting which flags came from the user

`internal/config/flags.go` already calls `fs.Parse`. After parsing, walk `fs.Visit` (which iterates only flags the user explicitly set — not defaults) and record names in a `map[string]bool`. Return this map alongside `Config`:

```go
func ParseFlags(args []string) (Config, map[string]bool, error)
```

`cmd/serve/main.go` passes the map to `ruleSet.MergeIntoConfig(&cfg, cliSet)`.

This is the cleanest way to distinguish "user didn't pass `-S`" from "user passed `-S=false`". Required for correct CLI > serve.json precedence.

### 7.2 Wiring in `cmd/serve/main.go`

```go
cfg, cliSet, err := config.ParseFlags(os.Args)
// ... existing help/version/directory resolution ...

ruleSet, err := rules.Load(cfg.ConfigFile, cfg.Directory)
if err != nil {
    log.Fatalf("serve.json: %v", err)
}
ruleSet.MergeIntoConfig(&cfg, cliSet)

// Optionally update fs root if `public` changed cfg.Directory.
h := logx.Middleware(log.Default(), cfg.NoRequestLogging)(
    handler.New(cfg, osDirFS(cfg.Directory), ruleSet),
)
```

### 7.3 Parse errors

Fail-fast at startup with `log.Fatalf("serve.json: %v", err)`. Error messages include the offending key (e.g., `"redirects[2].source: invalid path-to-regexp: ..."`). JSON syntax errors propagate from `encoding/json` with file path prepended.

## 8. Handler changes (`internal/handler/`)

- **`handler.go: New`** signature gains `ruleSet *rules.Set`. Middleware chain becomes:
  ```
  corsMiddleware(cfg.CORS) →
    ruleSet.Pre() →
      http.HandlerFunc(core.serve)
  ```
  where `core.serve` itself calls `ruleSet.Post()` wrapper inside `serveFile` before writing headers.
- **`handler.go: core` struct** gains `ruleSet *rules.Set` field, used by `core.serve` to query `IsListingEnabled` and `RenderSingle` before invoking `serveDirectory`, and by `serveDirectory` to filter `unlisted` entries.
- **`directory.go: serveDirectory`** signature gains `ruleSet *rules.Set` (or accept a filter callback). Drops entries whose `Name()` matches any `unlisted` pattern.
- **`Cache-Control` default removed:** F1 set `Cache-Control: no-cache` only on directory listings. F2 keeps that. The handler does **not** set `Cache-Control` on regular file responses — only `serve.json` `headers` rules can. This avoids conflicts with rule-set headers and matches `serve-handler`.

## 9. Testing

### 9.1 Unit (one `*_test.go` per source file)

- `match_test.go` — Section 5.1 matrix (~20 table cases).
- `rules_test.go` — parse success, parse failure with specific key in error, legacy `now.json` alias, no-config case.
- `pre_test.go` — one test per rule type and one combined-fixture test verifying order (redirects > rewrites > cleanUrls > trailingSlash).
- `post_test.go` — single header, multiple-matching-rules append in order, header value with `:param` capture, rule-set header overrides handler-set header.
- `listing_test.go` — `IsHidden`, `IsListingEnabled`, `RenderSingle` over crafted Sets.

### 9.2 Integration (extend `internal/handler/handler_test.go`)

- 301 redirect for `/old` → `/new`.
- Rewrite `/api/:id` → `/data/:id.json` serves correct file.
- `cleanUrls: true`: `/about` serves `/about.html`; `/about.html` → 301 `/about`.
- `trailingSlash: false`: `/dir/` → 301 `/dir` (overriding F1's default).
- Header rule `/*.css` adds `Cache-Control: max-age=31536000`.
- `directoryListing: false`: 404 on dir without index.
- `unlisted: [".git"]` hides `.git/` from listing HTML.
- `renderSingle: true`: dir with exactly one non-hidden file serves it.
- `public: "./dist"` redirects fs root.
- CLI override: `-S` wins over `symlinks: false` in config.

### 9.3 Golden fixtures (`internal/rules/testdata/`)

Four real-world `serve.json` files (sourced during implementation; candidates: Vue/Nuxt docs sites, Vercel example templates, Next.js static export examples). Each fixture:

```
testdata/<name>/
  serve.json
  files/
    index.html
    about.html
    api/users.json
    ...
  requests.json    # [{ "method": "GET", "url": "/api/users", "status": 200, "headers": {...}, "bodyContains": "..." }, ...]
```

Driver: `TestGoldenFixtures` walks `testdata/*/`, builds a `fstest.MapFS` from `files/`, loads `serve.json` via `rules.Load`, constructs a handler, runs each request, compares status / specified headers / `bodyContains`. New fixtures can be added without code changes.

### 9.4 E2E (`cmd/serve/serve_e2e_test.go`)

Extend with one test: writes a minimal `serve.json` (one redirect rule) into `t.TempDir()`, boots the binary with `-c`, asserts the redirect fires.

### 9.5 Spot check (manual, not automated)

Pick one npm-`serve` project from the wild (NOT one of the 4 fixtures), run `./serve -c <its>/serve.json` against its real file tree, verify a handful of routes return identical behavior to npm `serve`. Capture in PR description, not code.

## 10. Anticipated decisions / edge cases

- **Rewrite loop guard:** single rewrite per request. After one match, subsequent rewrites are skipped (matches `serve-handler` v6.1).
- **`headers` value capture expansion:** `value` may contain `:param`; `Expand` runs on it.
- **No default `Cache-Control` on regular files:** F1 already did not set `Cache-Control` on regular files (it only sets `no-cache` on directory listings). F2 keeps that behavior explicit: regular-file `Cache-Control` is owned entirely by `serve.json` `headers` rules. Avoids conflicts where a rule-set header would race with a handler-set one.
- **`public` resolution:** relative path resolved against `cfg.Directory` (the dir the user passed or cwd). Absolute paths used verbatim. If `public` points outside the original dir, that's allowed — npm `serve-handler` allows it and we match.
- **`unlisted` pattern target:** matches `entry.Name()` (filename only), not the full URL. The patterns in real `serve.json` files are usually filenames or simple globs.
- **Empty patterns:** `source: ""` is a parse error.
- **JSON unknown keys:** rejected (`json.Decoder.DisallowUnknownFields`) with a clear error. Prevents typos like `redirect:` (singular) silently being ignored.

## 11. Deliverables

1. `internal/rules/` package (7 `.go` + 7 `_test.go` + `testdata/` with 4 fixtures).
2. `handler.New` signature change to accept `*rules.Set`.
3. `config.ParseFlags` returns a `(Config, cliSet map[string]bool, error)` triple.
4. `cmd/serve/main.go` wires `rules.Load` + `MergeIntoConfig` + passes `*Set` to `handler.New`.
5. Integration tests in `internal/handler/handler_test.go` (~10 new cases).
6. E2E test extension in `cmd/serve/serve_e2e_test.go`.
7. README update with `serve.json` section + example.
8. Plan doc at `docs/superpowers/plans/2026-06-02-serve-go-phase2.md` (next step, via `writing-plans`).

**Unchanged:** `internal/{compress,logx,listener,mime}`, `internal/handler/{etag,headers,files,compress}.go` (logic preserved, just no longer setting default `Cache-Control`).

## 12. Phase 2 — Done criteria

- [ ] `go vet ./...` clean
- [ ] `go test ./... -race -count=1` green on Linux / macOS / Windows (CI)
- [ ] All 4 golden fixtures pass
- [ ] Manual spot-check: an external (non-fixture) `serve.json` from the wild produces identical behavior for documented routes
- [ ] Plan doc committed; README updated with `serve.json` reference
- [ ] Phase 2 squash-merged to `main`
