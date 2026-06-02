# serve Go — Phase 2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `serve.json` parity (rewrites, redirects, headers, cleanUrls, trailingSlash, directoryListing, unlisted, renderSingle, public, symlinks) to the Go reimplementation.

**Architecture:** New `internal/rules` package owns the parsed config (`*rules.Set`) and exposes a Pre middleware (redirects/rewrites/cleanUrls/trailingSlash), a Post middleware (headers), and three pure queries (IsHidden/IsListingEnabled/RenderSingle). The handler accepts `*rules.Set` as a third parameter and consults it during its existing decision points.

**Tech Stack:** Go 1.22+, stdlib only (`regexp`, `encoding/json`, `net/http`, `io/fs`, `testing/fstest`). No new external dependencies.

**Spec:** `docs/superpowers/specs/2026-06-02-serve-go-phase2-design.md`.

**Branch / worktree:** Use `superpowers:using-git-worktrees` to create branch `phase-2-rules` at worktree `.worktrees/phase-2-rules`.

---

## Task 1: `config.ParseFlags` returns a `cliSet` map

The CLI > serve.json precedence requires knowing which flags the user actually typed (not which have non-default values). `flag.FlagSet.Visit` walks only user-set flags after `Parse`.

**Files:**
- Modify: `internal/config/flags.go`
- Modify: `internal/config/flags_test.go`
- Modify: `cmd/serve/main.go` (caller)

- [ ] **Step 1: Write the failing test**

Add to `internal/config/flags_test.go`:

```go
func TestParseFlags_ReturnsCLISet(t *testing.T) {
	cfg, set, err := ParseFlags([]string{"serve", "-p", "8080", "-s", "."})
	if err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	if cfg.Port != 8080 {
		t.Fatalf("Port %d, want 8080", cfg.Port)
	}
	if !set["p"] {
		t.Fatalf("expected 'p' in cliSet, got %v", set)
	}
	if !set["s"] {
		t.Fatalf("expected 's' in cliSet, got %v", set)
	}
	if set["S"] {
		t.Fatalf("did not pass -S; should not be in cliSet")
	}
}
```

- [ ] **Step 2: Run the test and verify it fails**

Run: `go test ./internal/config/... -run TestParseFlags_ReturnsCLISet -v`
Expected: FAIL — `ParseFlags` returns 2 values, not 3.

- [ ] **Step 3: Update `ParseFlags` signature and existing callers**

In `internal/config/flags.go`, change the function so it returns `(Config, map[string]bool, error)` and walks `fs.Visit` after `fs.Parse`:

```go
func ParseFlags(args []string) (Config, map[string]bool, error) {
	var cfg Config
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	// ... existing flag definitions unchanged ...

	if err := fs.Parse(args[1:]); err != nil {
		return Config{}, nil, err
	}
	cliSet := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { cliSet[f.Name] = true })

	// ... existing positional-arg handling unchanged ...
	return cfg, cliSet, nil
}
```

Update every existing caller in `flags_test.go` to receive 3 return values (replace `_` where the map isn't needed). Update `cmd/serve/main.go`:

```go
cfg, cliSet, err := config.ParseFlags(os.Args)
if err != nil {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(2)
}
_ = cliSet // wired in Task 13
```

- [ ] **Step 4: Run all tests and verify they pass**

Run: `go test ./... -count=1`
Expected: all green.

- [ ] **Step 5: Commit**

```bash
git add internal/config/flags.go internal/config/flags_test.go cmd/serve/main.go
git commit -m "feat(config): ParseFlags returns cliSet for CLI > serve.json precedence"
```

---

## Task 2: `internal/rules/match.go` — path-to-regexp v6 subset

Compile `:name`, `:name?`, `:name+`, `:name*`, `*`, `**`, `(regex)`, and literals into a `*regexp.Regexp`. Match returns named captures. Expand substitutes `:name` and `$N` in destinations.

**Files:**
- Create: `internal/rules/match.go`
- Create: `internal/rules/match_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/rules/match_test.go`:

```go
package rules

import "testing"

func TestCompile_AndMatch(t *testing.T) {
	cases := []struct {
		name       string
		src        string
		url        string
		wantMatch  bool
		wantParams map[string]string
	}{
		{"literal match", "/about", "/about", true, map[string]string{}},
		{"literal no match", "/about", "/about/x", false, nil},
		{"named single segment", "/api/:id", "/api/42", true, map[string]string{"id": "42"}},
		{"named single segment no extra", "/api/:id", "/api/42/x", false, nil},
		{"two named segments", "/u/:user/p/:post", "/u/alice/p/7", true, map[string]string{"user": "alice", "post": "7"}},
		{"recursive wildcard", "/files/**", "/files/a/b/c", true, map[string]string{}},
		{"single-segment wildcard", "/x/*/y", "/x/zzz/y", true, map[string]string{}},
		{"single wildcard no nested", "/x/*/y", "/x/a/b/y", false, nil},
		{"optional present", "/a/:b?", "/a/hello", true, map[string]string{"b": "hello"}},
		{"optional absent", "/a/:b?", "/a", true, map[string]string{"b": ""}},
		{"one-or-more captures slashes", "/r/:rest+", "/r/a/b/c", true, map[string]string{"rest": "a/b/c"}},
		{"literal regex metachars escaped", "/v1.0/:x", "/v1.0/foo", true, map[string]string{"x": "foo"}},
		{"literal regex metachars no false match", "/v1.0/:x", "/v100/foo", false, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p, err := Compile(c.src)
			if err != nil {
				t.Fatalf("Compile(%q): %v", c.src, err)
			}
			params, ok := p.Match(c.url)
			if ok != c.wantMatch {
				t.Fatalf("Match(%q) ok=%v want %v", c.url, ok, c.wantMatch)
			}
			if !ok {
				return
			}
			for k, v := range c.wantParams {
				if params[k] != v {
					t.Fatalf("param[%q]=%q want %q (full=%v)", k, params[k], v, params)
				}
			}
		})
	}
}

func TestCompile_RejectsEmpty(t *testing.T) {
	if _, err := Compile(""); err == nil {
		t.Fatal("expected error for empty pattern")
	}
}

func TestExpand_NamedCapture(t *testing.T) {
	p, _ := Compile("/old/:slug")
	params, _ := p.Match("/old/hello")
	got := p.Expand("/new/:slug", params)
	if got != "/new/hello" {
		t.Fatalf("Expand: %q want /new/hello", got)
	}
}

func TestExpand_PositionalCapture(t *testing.T) {
	p, _ := Compile("/a/:x/b/:y")
	params, _ := p.Match("/a/1/b/2")
	got := p.Expand("/swap/$2/$1", params)
	if got != "/swap/2/1" {
		t.Fatalf("Expand: %q want /swap/2/1", got)
	}
}

func TestExpand_MissingParamKeepsLiteral(t *testing.T) {
	p, _ := Compile("/a/:x")
	got := p.Expand("/b/:nope", map[string]string{})
	if got != "/b/:nope" {
		t.Fatalf("Expand: %q want /b/:nope (literal pass-through)", got)
	}
	_ = p
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/rules/... -v`
Expected: FAIL — package doesn't exist yet.

- [ ] **Step 3: Implement `match.go`**

Create `internal/rules/match.go`:

```go
package rules

import (
	"fmt"
	"regexp"
	"strings"
)

// Pattern is a compiled path-to-regexp v6 subset pattern.
type Pattern struct {
	re   *regexp.Regexp
	keys []string // ordered named captures (no "" entries)
}

// Compile turns a path-to-regexp v6 subset source into a *Pattern.
// Supported:
//   :name        one segment, named capture
//   :name?       optional segment (consumes preceding "/")
//   :name+       one or more segments
//   :name*       zero or more segments
//   *            wildcard one segment, no capture
//   **           recursive wildcard, no capture
//   (regex)      inline regex (passed through verbatim)
//   anything else: literal (regex-escaped)
func Compile(src string) (*Pattern, error) {
	if src == "" {
		return nil, fmt.Errorf("empty pattern")
	}
	var b strings.Builder
	var keys []string
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == ':':
			i++
			start := i
			for i < len(src) && isNameChar(src[i]) {
				i++
			}
			name := src[start:i]
			if name == "" {
				return nil, fmt.Errorf("empty capture name at offset %d", start-1)
			}
			modifier := byte(0)
			if i < len(src) && (src[i] == '?' || src[i] == '+' || src[i] == '*') {
				modifier = src[i]
				i++
			}
			keys = append(keys, name)
			switch modifier {
			case 0:
				fmt.Fprintf(&b, `(?P<%s>[^/]+)`, name)
			case '?':
				cur := b.String()
				if strings.HasSuffix(cur, "/") {
					trimmed := cur[:len(cur)-1]
					b.Reset()
					b.WriteString(trimmed)
					fmt.Fprintf(&b, `(?:/(?P<%s>[^/]+))?`, name)
				} else {
					fmt.Fprintf(&b, `(?P<%s>[^/]+)?`, name)
				}
			case '+':
				fmt.Fprintf(&b, `(?P<%s>[^/].*?)`, name)
			case '*':
				fmt.Fprintf(&b, `(?P<%s>.*)?`, name)
			}
		case c == '*':
			if i+1 < len(src) && src[i+1] == '*' {
				b.WriteString(`.*`)
				i += 2
			} else {
				b.WriteString(`[^/]+`)
				i++
			}
		case c == '(':
			j := i + 1
			depth := 1
			for j < len(src) && depth > 0 {
				switch src[j] {
				case '(':
					depth++
				case ')':
					depth--
				}
				if depth > 0 {
					j++
				}
			}
			if depth != 0 {
				return nil, fmt.Errorf("unmatched ( at offset %d", i)
			}
			b.WriteString(src[i : j+1])
			i = j + 1
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
			i++
		}
	}
	full := "^" + b.String() + "$"
	re, err := regexp.Compile(full)
	if err != nil {
		return nil, fmt.Errorf("compile %q -> %q: %w", src, full, err)
	}
	return &Pattern{re: re, keys: keys}, nil
}

func isNameChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') || c == '_'
}

// Match reports whether urlPath matches and, if so, returns named captures.
// An optional `:name?` that didn't match yields the empty string for that name.
func (p *Pattern) Match(urlPath string) (map[string]string, bool) {
	m := p.re.FindStringSubmatch(urlPath)
	if m == nil {
		return nil, false
	}
	out := make(map[string]string, len(p.keys))
	for i, name := range p.re.SubexpNames() {
		if i == 0 || name == "" {
			continue
		}
		out[name] = m[i]
	}
	for _, k := range p.keys {
		if _, ok := out[k]; !ok {
			out[k] = ""
		}
	}
	return out, true
}

// Expand substitutes :name and $N tokens in dest with values from params.
// Missing tokens are left as literal :name or $N.
func (p *Pattern) Expand(dest string, params map[string]string) string {
	if dest == "" {
		return dest
	}
	var b strings.Builder
	i := 0
	for i < len(dest) {
		c := dest[i]
		switch {
		case c == ':' && i+1 < len(dest) && isNameChar(dest[i+1]):
			i++
			start := i
			for i < len(dest) && isNameChar(dest[i]) {
				i++
			}
			name := dest[start:i]
			if v, ok := params[name]; ok {
				b.WriteString(v)
			} else {
				b.WriteByte(':')
				b.WriteString(name)
			}
		case c == '$' && i+1 < len(dest) && dest[i+1] >= '0' && dest[i+1] <= '9':
			i++
			start := i
			for i < len(dest) && dest[i] >= '0' && dest[i] <= '9' {
				i++
			}
			num := dest[start:i]
			idx := 0
			for _, ch := range []byte(num) {
				idx = idx*10 + int(ch-'0')
			}
			if idx >= 1 && idx <= len(p.keys) {
				key := p.keys[idx-1]
				if v, ok := params[key]; ok {
					b.WriteString(v)
					continue
				}
			}
			b.WriteByte('$')
			b.WriteString(num)
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/rules/... -v -run Compile`
Run: `go test ./internal/rules/... -v -run Expand`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/rules/match.go internal/rules/match_test.go
git commit -m "feat(rules): path-to-regexp v6 subset matcher (Compile/Match/Expand)"
```

---

## Task 3: `internal/rules/rules.go` — `Set` struct + `Load`

The schema reader. Reads `serve.json` or `now.json` (legacy alias). Rejects unknown JSON keys. Pre-compiles all patterns into `*Pattern` so runtime is allocation-free.

**Files:**
- Create: `internal/rules/rules.go`
- Create: `internal/rules/rules_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/rules/rules_test.go`:

```go
package rules

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return dir
}

func TestLoad_NoConfigReturnsEmptySet(t *testing.T) {
	s, err := Load("", t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s == nil {
		t.Fatal("nil Set")
	}
	if !s.IsListingEnabled("/x") {
		t.Fatal("empty Set should default IsListingEnabled to true")
	}
}

func TestLoad_ServeJson(t *testing.T) {
	dir := writeTemp(t, "serve.json", `{
		"public": "./dist",
		"redirects": [{"source": "/old", "destination": "/new"}],
		"rewrites": [{"source": "/api/:id", "destination": "/data/:id.json"}],
		"headers": [{"source": "/*.css", "headers": [{"key": "Cache-Control", "value": "max-age=60"}]}],
		"cleanUrls": true,
		"trailingSlash": false,
		"directoryListing": true,
		"unlisted": [".git"],
		"renderSingle": false,
		"symlinks": false
	}`)
	s, err := Load("", dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Public() != "./dist" {
		t.Fatalf("Public %q", s.Public())
	}
	if len(s.Redirects()) != 1 {
		t.Fatalf("got %d redirects", len(s.Redirects()))
	}
}

func TestLoad_LegacyNowJson(t *testing.T) {
	dir := writeTemp(t, "now.json", `{"public": "./out"}`)
	s, err := Load("", dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Public() != "./out" {
		t.Fatalf("Public %q", s.Public())
	}
}

func TestLoad_ExplicitPathWinsOverDirAuto(t *testing.T) {
	dir := writeTemp(t, "serve.json", `{"public": "./auto"}`)
	other := filepath.Join(t.TempDir(), "custom.json")
	if err := os.WriteFile(other, []byte(`{"public": "./custom"}`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	s, err := Load(other, dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Public() != "./custom" {
		t.Fatalf("Public %q want ./custom", s.Public())
	}
}

func TestLoad_RejectsUnknownKey(t *testing.T) {
	dir := writeTemp(t, "serve.json", `{"redirectz": []}`)
	if _, err := Load("", dir); err == nil {
		t.Fatal("expected error for unknown key 'redirectz'")
	}
}

func TestLoad_InvalidPatternErrors(t *testing.T) {
	dir := writeTemp(t, "serve.json", `{"redirects": [{"source": "", "destination": "/x"}]}`)
	if _, err := Load("", dir); err == nil {
		t.Fatal("expected error for empty source pattern")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/rules/... -v -run Load`
Expected: FAIL — `Load`, `Public`, `Redirects` undefined.

- [ ] **Step 3: Implement `rules.go`**

Create `internal/rules/rules.go`:

```go
package rules

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Set is the parsed, compiled view of a serve.json file.
type Set struct {
	publicDir       string
	symlinks        bool
	symlinksSet     bool
	publicSet       bool
	cleanUrls       cleanUrlsValue
	trailingSlash   *bool
	directoryList   directoryListingValue
	renderSingle    bool
	unlisted        []*Pattern
	redirects       []Redirect
	rewrites        []Rewrite
	headers         []HeaderRule
	exists          func(string) bool
}

// Redirect is one parsed entry from "redirects".
type Redirect struct {
	Pattern     *Pattern
	Destination string
	Status      int // 301 default, can be 302/307/308
}

// Rewrite is one parsed entry from "rewrites".
type Rewrite struct {
	Pattern     *Pattern
	Destination string
}

// HeaderRule applies a set of {key,value} pairs to URLs matching Pattern.
type HeaderRule struct {
	Pattern *Pattern
	Headers []HeaderKV
}

type HeaderKV struct{ Key, Value string }

// cleanUrlsValue captures bool-or-pattern-array semantics.
type cleanUrlsValue struct {
	enabled  bool
	patterns []*Pattern // empty means "applies to all" when enabled
}

type directoryListingValue struct {
	hasValue bool
	enabled  bool
	patterns []*Pattern
}

// rawSchema mirrors the JSON shape of serve.json before pattern compilation.
type rawSchema struct {
	Public           *string         `json:"public,omitempty"`
	Symlinks         *bool           `json:"symlinks,omitempty"`
	CleanUrls        json.RawMessage `json:"cleanUrls,omitempty"`
	TrailingSlash    *bool           `json:"trailingSlash,omitempty"`
	DirectoryListing json.RawMessage `json:"directoryListing,omitempty"`
	RenderSingle     *bool           `json:"renderSingle,omitempty"`
	Unlisted         []string        `json:"unlisted,omitempty"`
	Redirects        []rawRedirect   `json:"redirects,omitempty"`
	Rewrites         []rawRewrite    `json:"rewrites,omitempty"`
	Headers          []rawHeader     `json:"headers,omitempty"`
}

type rawRedirect struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Type        int    `json:"type,omitempty"`
}

type rawRewrite struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
}

type rawHeader struct {
	Source  string     `json:"source"`
	Headers []HeaderKV `json:"headers"`
}

// Load resolves the config in this order:
//   1. configFile if non-empty (must exist; missing → error).
//   2. <dir>/serve.json
//   3. <dir>/now.json (legacy alias)
//   4. nothing → returns &Set{} (no-op).
func Load(configFile, dir string) (*Set, error) {
	var path string
	switch {
	case configFile != "":
		path = configFile
	default:
		for _, name := range []string{"serve.json", "now.json"} {
			p := filepath.Join(dir, name)
			if _, err := os.Stat(p); err == nil {
				path = p
				break
			}
		}
	}
	if path == "" {
		return &Set{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", path, err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var raw rawSchema
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("parse %q: %w", path, err)
	}
	return compile(raw)
}

func compile(raw rawSchema) (*Set, error) {
	s := &Set{}
	if raw.Public != nil {
		s.publicDir = *raw.Public
		s.publicSet = true
	}
	if raw.Symlinks != nil {
		s.symlinks = *raw.Symlinks
		s.symlinksSet = true
	}
	if raw.TrailingSlash != nil {
		v := *raw.TrailingSlash
		s.trailingSlash = &v
	}
	if raw.RenderSingle != nil {
		s.renderSingle = *raw.RenderSingle
	}
	if len(raw.CleanUrls) > 0 {
		cv, err := parseCleanUrls(raw.CleanUrls)
		if err != nil {
			return nil, fmt.Errorf("cleanUrls: %w", err)
		}
		s.cleanUrls = cv
	}
	if len(raw.DirectoryListing) > 0 {
		dv, err := parseDirectoryListing(raw.DirectoryListing)
		if err != nil {
			return nil, fmt.Errorf("directoryListing: %w", err)
		}
		s.directoryList = dv
	}
	for i, src := range raw.Unlisted {
		p, err := Compile(src)
		if err != nil {
			return nil, fmt.Errorf("unlisted[%d]: %w", i, err)
		}
		s.unlisted = append(s.unlisted, p)
	}
	for i, r := range raw.Redirects {
		p, err := Compile(r.Source)
		if err != nil {
			return nil, fmt.Errorf("redirects[%d].source: %w", i, err)
		}
		status := r.Type
		if status == 0 {
			status = 301
		}
		s.redirects = append(s.redirects, Redirect{Pattern: p, Destination: r.Destination, Status: status})
	}
	for i, r := range raw.Rewrites {
		p, err := Compile(r.Source)
		if err != nil {
			return nil, fmt.Errorf("rewrites[%d].source: %w", i, err)
		}
		s.rewrites = append(s.rewrites, Rewrite{Pattern: p, Destination: r.Destination})
	}
	for i, h := range raw.Headers {
		p, err := Compile(h.Source)
		if err != nil {
			return nil, fmt.Errorf("headers[%d].source: %w", i, err)
		}
		s.headers = append(s.headers, HeaderRule{Pattern: p, Headers: h.Headers})
	}
	return s, nil
}

func parseCleanUrls(raw json.RawMessage) (cleanUrlsValue, error) {
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		return cleanUrlsValue{enabled: b}, nil
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err != nil {
		return cleanUrlsValue{}, fmt.Errorf("expected bool or []string")
	}
	cv := cleanUrlsValue{enabled: true}
	for i, src := range list {
		p, err := Compile(src)
		if err != nil {
			return cleanUrlsValue{}, fmt.Errorf("[%d]: %w", i, err)
		}
		cv.patterns = append(cv.patterns, p)
	}
	return cv, nil
}

func parseDirectoryListing(raw json.RawMessage) (directoryListingValue, error) {
	var b bool
	if err := json.Unmarshal(raw, &b); err == nil {
		return directoryListingValue{hasValue: true, enabled: b}, nil
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err != nil {
		return directoryListingValue{}, fmt.Errorf("expected bool or []string")
	}
	dv := directoryListingValue{hasValue: true, enabled: true}
	for i, src := range list {
		p, err := Compile(src)
		if err != nil {
			return directoryListingValue{}, fmt.Errorf("[%d]: %w", i, err)
		}
		dv.patterns = append(dv.patterns, p)
	}
	return dv, nil
}

// --- Accessors (used by handler + cmd/serve) ---

func (s *Set) Public() string {
	if s == nil {
		return ""
	}
	return s.publicDir
}

func (s *Set) Redirects() []Redirect {
	if s == nil {
		return nil
	}
	return s.redirects
}

func (s *Set) Rewrites() []Rewrite {
	if s == nil {
		return nil
	}
	return s.rewrites
}

func (s *Set) Headers() []HeaderRule {
	if s == nil {
		return nil
	}
	return s.headers
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/rules/... -v -run Load`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/rules/rules.go internal/rules/rules_test.go
git commit -m "feat(rules): Set + Load (serve.json + legacy now.json) with pattern compilation"
```

---

## Task 4: `internal/rules/rules.go` — `MergeIntoConfig`

Apply `public` and `symlinks` onto `*config.Config` only when the user didn't set the corresponding CLI flag.

**Files:**
- Modify: `internal/rules/rules.go`
- Create: `internal/rules/merge_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/rules/merge_test.go`:

```go
package rules

import (
	"testing"

	"serve/internal/config"
)

func TestMergeIntoConfig_PublicAppliedWhenNoCLIFlag(t *testing.T) {
	s := &Set{publicDir: "./dist", publicSet: true}
	cfg := &config.Config{Directory: "/cwd"}
	s.MergeIntoConfig(cfg, map[string]bool{})
	if cfg.Directory != "./dist" {
		t.Fatalf("Directory %q want ./dist", cfg.Directory)
	}
}

func TestMergeIntoConfig_PublicIgnoredWhenCLISet(t *testing.T) {
	s := &Set{publicDir: "./dist", publicSet: true}
	cfg := &config.Config{Directory: "/cwd"}
	s.MergeIntoConfig(cfg, map[string]bool{"d": true})
	if cfg.Directory != "/cwd" {
		t.Fatalf("Directory %q want /cwd (CLI -d wins)", cfg.Directory)
	}
}

func TestMergeIntoConfig_SymlinksAppliedWhenNoCLIFlag(t *testing.T) {
	s := &Set{symlinks: true, symlinksSet: true}
	cfg := &config.Config{}
	s.MergeIntoConfig(cfg, map[string]bool{})
	if !cfg.Symlinks {
		t.Fatal("expected Symlinks=true from serve.json")
	}
}

func TestMergeIntoConfig_SymlinksIgnoredWhenCLISet(t *testing.T) {
	s := &Set{symlinks: true, symlinksSet: true}
	cfg := &config.Config{Symlinks: false}
	s.MergeIntoConfig(cfg, map[string]bool{"S": true, "symlinks": true})
	if cfg.Symlinks {
		t.Fatal("expected Symlinks=false (CLI -S=false wins over serve.json true)")
	}
}

func TestMergeIntoConfig_NilSetIsNoOp(t *testing.T) {
	var s *Set
	cfg := &config.Config{Directory: "/cwd"}
	s.MergeIntoConfig(cfg, map[string]bool{})
	if cfg.Directory != "/cwd" {
		t.Fatal("nil Set should be no-op")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/rules/... -v -run Merge`
Expected: FAIL — `MergeIntoConfig` undefined.

- [ ] **Step 3: Implement `MergeIntoConfig`**

Append to `internal/rules/rules.go`:

```go
// MergeIntoConfig applies serve.json overrides for `public` and `symlinks`
// onto cfg, only when the user did NOT set the corresponding CLI flag.
// Pass the cliSet map returned by config.ParseFlags. Safe to call on nil *Set.
func (s *Set) MergeIntoConfig(cfg *config.Config, cliSet map[string]bool) {
	if s == nil || cfg == nil {
		return
	}
	if s.publicSet && !cliSet["d"] {
		cfg.Directory = s.publicDir
	}
	if s.symlinksSet && !cliSet["S"] && !cliSet["symlinks"] {
		cfg.Symlinks = s.symlinks
	}
}
```

Add the import to the top of the file:

```go
import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"serve/internal/config"
)
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/rules/... -v -run Merge`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/rules/rules.go internal/rules/merge_test.go
git commit -m "feat(rules): MergeIntoConfig respects CLI > serve.json precedence"
```

---

## Task 5: `internal/rules/pre.go` — redirects sub-handler

First match wins. Status defaults to 301. Destination is `Expand`ed with captures.

**Files:**
- Create: `internal/rules/pre.go`
- Create: `internal/rules/pre_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/rules/pre_test.go`:

```go
package rules

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func mustCompile(t *testing.T, src string) *Pattern {
	t.Helper()
	p, err := Compile(src)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	return p
}

func TestPre_RedirectSimple(t *testing.T) {
	s := &Set{redirects: []Redirect{
		{Pattern: mustCompile(t, "/old"), Destination: "/new", Status: 301},
	}}
	h := s.Pre()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next should not be called")
	}))
	req := httptest.NewRequest("GET", "/old", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 301 {
		t.Fatalf("status %d, want 301", rec.Code)
	}
	if rec.Header().Get("Location") != "/new" {
		t.Fatalf("Location %q", rec.Header().Get("Location"))
	}
}

func TestPre_RedirectWithCapture(t *testing.T) {
	s := &Set{redirects: []Redirect{
		{Pattern: mustCompile(t, "/u/:id"), Destination: "/users/:id", Status: 301},
	}}
	h := s.Pre()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	req := httptest.NewRequest("GET", "/u/42", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Location") != "/users/42" {
		t.Fatalf("Location %q, want /users/42", rec.Header().Get("Location"))
	}
}

func TestPre_RedirectStatusOverride(t *testing.T) {
	s := &Set{redirects: []Redirect{
		{Pattern: mustCompile(t, "/x"), Destination: "/y", Status: 308},
	}}
	h := s.Pre()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	if rec.Code != 308 {
		t.Fatalf("status %d, want 308", rec.Code)
	}
}

func TestPre_NoMatchCallsNext(t *testing.T) {
	s := &Set{redirects: []Redirect{
		{Pattern: mustCompile(t, "/old"), Destination: "/new", Status: 301},
	}}
	called := false
	h := s.Pre()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/other", nil))
	if !called {
		t.Fatal("expected next handler to be called for non-matching URL")
	}
}

func TestPre_NilSetPassThrough(t *testing.T) {
	var s *Set
	called := false
	h := s.Pre()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/anything", nil))
	if !called {
		t.Fatal("nil Set.Pre() should be a pass-through")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/rules/... -v -run Pre_Redirect`
Expected: FAIL — `Pre` method undefined.

- [ ] **Step 3: Implement the redirects branch of `Pre`**

Create `internal/rules/pre.go`:

```go
package rules

import "net/http"

// Pre returns the middleware applied between CORS and the file-serving core.
// Order inside: redirects → rewrites → cleanUrls → trailingSlash → next.
// Safe to call on nil *Set (returns a pass-through middleware).
func (s *Set) Pre() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if s == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if s.handleRedirect(w, r) {
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (s *Set) handleRedirect(w http.ResponseWriter, r *http.Request) bool {
	for _, rd := range s.redirects {
		params, ok := rd.Pattern.Match(r.URL.Path)
		if !ok {
			continue
		}
		dest := rd.Pattern.Expand(rd.Destination, params)
		http.Redirect(w, r, dest, rd.Status)
		return true
	}
	return false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/rules/... -v -run Pre_`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/rules/pre.go internal/rules/pre_test.go
git commit -m "feat(rules): Pre middleware with redirects branch (first match wins)"
```

---

## Task 6: `internal/rules/pre.go` — rewrites with single-loop guard

Mutates `r.URL.Path`. Single rewrite per request (later rewrites are skipped after the first match fires).

**Files:**
- Modify: `internal/rules/pre.go`
- Modify: `internal/rules/pre_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/rules/pre_test.go`:

```go
func TestPre_RewriteMutatesURL(t *testing.T) {
	s := &Set{rewrites: []Rewrite{
		{Pattern: mustCompile(t, "/api/:id"), Destination: "/data/:id.json"},
	}}
	got := ""
	h := s.Pre()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/42", nil))
	if got != "/data/42.json" {
		t.Fatalf("rewritten path %q, want /data/42.json", got)
	}
}

func TestPre_RewriteSingleLoop(t *testing.T) {
	// Two rewrites that would loop A→B→A if the loop runs more than once.
	s := &Set{rewrites: []Rewrite{
		{Pattern: mustCompile(t, "/A"), Destination: "/B"},
		{Pattern: mustCompile(t, "/B"), Destination: "/A"},
	}}
	got := ""
	h := s.Pre()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/A", nil))
	if got != "/B" {
		t.Fatalf("rewritten %q, want /B (rewrite must not re-run)", got)
	}
}

func TestPre_RewriteNoMatchUntouched(t *testing.T) {
	s := &Set{rewrites: []Rewrite{
		{Pattern: mustCompile(t, "/api/:id"), Destination: "/data/:id.json"},
	}}
	got := ""
	h := s.Pre()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/static/x.css", nil))
	if got != "/static/x.css" {
		t.Fatalf("path mutated to %q, want untouched", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/rules/... -v -run Pre_Rewrite`
Expected: FAIL — rewrites not yet handled.

- [ ] **Step 3: Add the rewrites stage**

Modify the `Pre` handler chain to call rewrites between redirects and next:

```go
func (s *Set) Pre() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if s == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if s.handleRedirect(w, r) {
				return
			}
			s.applyRewrite(r)
			next.ServeHTTP(w, r)
		})
	}
}

// applyRewrite mutates r.URL.Path with the first matching rewrite's expanded
// destination. At most one rewrite per request — subsequent rewrites are not
// re-evaluated even if the new path matches another rule.
func (s *Set) applyRewrite(r *http.Request) {
	for _, rw := range s.rewrites {
		params, ok := rw.Pattern.Match(r.URL.Path)
		if !ok {
			continue
		}
		r.URL.Path = rw.Pattern.Expand(rw.Destination, params)
		return
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/rules/... -v -run Pre_`
Expected: all PASS (redirects + rewrites cases green).

- [ ] **Step 5: Commit**

```bash
git add internal/rules/pre.go internal/rules/pre_test.go
git commit -m "feat(rules): Pre rewrites with single-loop guard"
```

---

## Task 7: `internal/rules/pre.go` — cleanUrls + SetExists

`/about` → rewrite to `/about.html` when fs has it. `/about.html` → 301 to `/about`. Existence check via injectable callback.

**Files:**
- Modify: `internal/rules/pre.go`
- Modify: `internal/rules/rules.go` (add `SetExists` + `cleanUrls` query)
- Modify: `internal/rules/pre_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/rules/pre_test.go`:

```go
func TestPre_CleanUrlsRewritesToHTML(t *testing.T) {
	s := &Set{cleanUrls: cleanUrlsValue{enabled: true}}
	s.SetExists(func(p string) bool { return p == "/about.html" })
	got := ""
	h := s.Pre()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/about", nil))
	if got != "/about.html" {
		t.Fatalf("path %q, want /about.html", got)
	}
}

func TestPre_CleanUrlsRedirectsAwayFromHTML(t *testing.T) {
	s := &Set{cleanUrls: cleanUrlsValue{enabled: true}}
	s.SetExists(func(p string) bool { return p == "/about.html" })
	h := s.Pre()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next should not be called")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/about.html", nil))
	if rec.Code != 301 {
		t.Fatalf("status %d, want 301", rec.Code)
	}
	if rec.Header().Get("Location") != "/about" {
		t.Fatalf("Location %q", rec.Header().Get("Location"))
	}
}

func TestPre_CleanUrlsDisabled(t *testing.T) {
	s := &Set{cleanUrls: cleanUrlsValue{enabled: false}}
	s.SetExists(func(p string) bool { return p == "/about.html" })
	got := ""
	h := s.Pre()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/about", nil))
	if got != "/about" {
		t.Fatalf("path %q, want /about (cleanUrls disabled)", got)
	}
}

func TestPre_CleanUrlsNoExistsCallbackIsNoOp(t *testing.T) {
	s := &Set{cleanUrls: cleanUrlsValue{enabled: true}}
	got := ""
	h := s.Pre()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/about", nil))
	if got != "/about" {
		t.Fatalf("path %q, want /about (no SetExists)", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/rules/... -v -run Pre_CleanUrls`
Expected: FAIL — `SetExists` undefined; cleanUrls not honored.

- [ ] **Step 3: Implement cleanUrls in `pre.go` and `SetExists` in `rules.go`**

Append to `internal/rules/rules.go`:

```go
// SetExists installs the filesystem-existence check used by cleanUrls.
// Pass nil to clear. Safe on nil *Set.
func (s *Set) SetExists(fn func(urlPath string) bool) {
	if s == nil {
		return
	}
	s.exists = fn
}

func (cv cleanUrlsValue) appliesTo(urlPath string) bool {
	if !cv.enabled {
		return false
	}
	if len(cv.patterns) == 0 {
		return true
	}
	for _, p := range cv.patterns {
		if _, ok := p.Match(urlPath); ok {
			return true
		}
	}
	return false
}
```

Insert a new stage into `Pre` between rewrites and the next call:

```go
func (s *Set) Pre() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if s == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if s.handleRedirect(w, r) {
				return
			}
			s.applyRewrite(r)
			if s.handleCleanUrls(w, r) {
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// handleCleanUrls returns true if it wrote a response (the 301 case).
// Returns false but may mutate r.URL.Path (the internal rewrite case).
func (s *Set) handleCleanUrls(w http.ResponseWriter, r *http.Request) bool {
	if !s.cleanUrls.appliesTo(r.URL.Path) || s.exists == nil {
		return false
	}
	p := r.URL.Path
	if hasHTMLSuffix(p) {
		stripped := p[:len(p)-len(".html")]
		if stripped == "" {
			stripped = "/"
		}
		if s.exists(p) {
			http.Redirect(w, r, stripped, http.StatusMovedPermanently)
			return true
		}
		return false
	}
	if p == "" || p[len(p)-1] == '/' {
		return false
	}
	candidate := p + ".html"
	if s.exists(candidate) {
		r.URL.Path = candidate
	}
	return false
}

func hasHTMLSuffix(p string) bool {
	const s = ".html"
	return len(p) >= len(s) && p[len(p)-len(s):] == s
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/rules/... -v -run Pre_CleanUrls`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/rules/pre.go internal/rules/rules.go internal/rules/pre_test.go
git commit -m "feat(rules): cleanUrls (with SetExists callback) in Pre chain"
```

---

## Task 8: `internal/rules/pre.go` — trailingSlash

`trailingSlash: false` strips `/dir/` → 301 `/dir`. `trailingSlash: true` adds `/x` → 301 `/x/` (only when `/x` has no extension, to avoid `/style.css/`).

**Files:**
- Modify: `internal/rules/pre.go`
- Modify: `internal/rules/pre_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/rules/pre_test.go`:

```go
func TestPre_TrailingSlashStrip(t *testing.T) {
	f := false
	s := &Set{trailingSlash: &f}
	h := s.Pre()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next should not be called")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/dir/", nil))
	if rec.Code != 301 {
		t.Fatalf("status %d, want 301", rec.Code)
	}
	if rec.Header().Get("Location") != "/dir" {
		t.Fatalf("Location %q, want /dir", rec.Header().Get("Location"))
	}
}

func TestPre_TrailingSlashAdd(t *testing.T) {
	v := true
	s := &Set{trailingSlash: &v}
	h := s.Pre()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next should not be called")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/dir", nil))
	if rec.Code != 301 {
		t.Fatalf("status %d, want 301", rec.Code)
	}
	if rec.Header().Get("Location") != "/dir/" {
		t.Fatalf("Location %q, want /dir/", rec.Header().Get("Location"))
	}
}

func TestPre_TrailingSlashAddSkipsExtensions(t *testing.T) {
	v := true
	s := &Set{trailingSlash: &v}
	called := false
	h := s.Pre()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/style.css", nil))
	if !called {
		t.Fatal("file with extension should not be redirected to add trailing slash")
	}
}

func TestPre_TrailingSlashUnsetIsNoOp(t *testing.T) {
	s := &Set{}
	called := false
	h := s.Pre()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/dir/", nil))
	if !called {
		t.Fatal("absent trailingSlash should not affect requests")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/rules/... -v -run Pre_TrailingSlash`
Expected: FAIL.

- [ ] **Step 3: Implement the trailingSlash stage**

Add another step in `Pre` between cleanUrls and `next`:

```go
func (s *Set) Pre() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if s == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if s.handleRedirect(w, r) {
				return
			}
			s.applyRewrite(r)
			if s.handleCleanUrls(w, r) {
				return
			}
			if s.handleTrailingSlash(w, r) {
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (s *Set) handleTrailingSlash(w http.ResponseWriter, r *http.Request) bool {
	if s.trailingSlash == nil {
		return false
	}
	p := r.URL.Path
	if p == "" || p == "/" {
		return false
	}
	hasSlash := p[len(p)-1] == '/'
	if !*s.trailingSlash && hasSlash {
		http.Redirect(w, r, p[:len(p)-1], http.StatusMovedPermanently)
		return true
	}
	if *s.trailingSlash && !hasSlash && !hasExt(p) {
		http.Redirect(w, r, p+"/", http.StatusMovedPermanently)
		return true
	}
	return false
}

// hasExt reports whether the last path segment contains a "." (likely a file).
// Used to skip the trailing-slash add for /style.css etc.
func hasExt(p string) bool {
	for i := len(p) - 1; i >= 0; i-- {
		switch p[i] {
		case '/':
			return false
		case '.':
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/rules/... -v -run Pre_TrailingSlash`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/rules/pre.go internal/rules/pre_test.go
git commit -m "feat(rules): trailingSlash add/strip stage in Pre chain"
```

---

## Task 9: `internal/rules/post.go` — headers middleware

Wraps `ResponseWriter`. On the first `WriteHeader` (or `Write` if `WriteHeader` was never called), inject every matching rule's headers, in order. Rule-set headers override handler-set headers (because they are written last).

**Files:**
- Create: `internal/rules/post.go`
- Create: `internal/rules/post_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/rules/post_test.go`:

```go
package rules

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPost_AddsHeader(t *testing.T) {
	s := &Set{headers: []HeaderRule{{
		Pattern: mustCompile(t, "/*.css"),
		Headers: []HeaderKV{{Key: "Cache-Control", Value: "max-age=60"}},
	}}}
	h := s.Post()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/style.css", nil))
	if got := rec.Header().Get("Cache-Control"); got != "max-age=60" {
		t.Fatalf("Cache-Control %q", got)
	}
}

func TestPost_NoMatchNoChange(t *testing.T) {
	s := &Set{headers: []HeaderRule{{
		Pattern: mustCompile(t, "/*.css"),
		Headers: []HeaderKV{{Key: "Cache-Control", Value: "max-age=60"}},
	}}}
	h := s.Post()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/style.js", nil))
	if rec.Header().Get("Cache-Control") != "" {
		t.Fatalf("unexpected Cache-Control on non-match")
	}
}

func TestPost_MultipleMatchingRulesAllApply(t *testing.T) {
	s := &Set{headers: []HeaderRule{
		{Pattern: mustCompile(t, "/**"), Headers: []HeaderKV{{Key: "X-One", Value: "a"}}},
		{Pattern: mustCompile(t, "/x"), Headers: []HeaderKV{{Key: "X-Two", Value: "b"}}},
	}}
	h := s.Post()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	if rec.Header().Get("X-One") != "a" || rec.Header().Get("X-Two") != "b" {
		t.Fatalf("X-One=%q X-Two=%q", rec.Header().Get("X-One"), rec.Header().Get("X-Two"))
	}
}

func TestPost_ValueExpansionWithCapture(t *testing.T) {
	s := &Set{headers: []HeaderRule{{
		Pattern: mustCompile(t, "/u/:id"),
		Headers: []HeaderKV{{Key: "X-User", Value: ":id"}},
	}}}
	h := s.Post()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/u/42", nil))
	if rec.Header().Get("X-User") != "42" {
		t.Fatalf("X-User %q, want 42", rec.Header().Get("X-User"))
	}
}

func TestPost_OverridesHandlerHeader(t *testing.T) {
	s := &Set{headers: []HeaderRule{{
		Pattern: mustCompile(t, "/**"),
		Headers: []HeaderKV{{Key: "Content-Type", Value: "text/x-custom"}},
	}}}
	h := s.Post()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(200)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/x", nil))
	if got := rec.Header().Get("Content-Type"); got != "text/x-custom" {
		t.Fatalf("Content-Type %q, rule should win over handler", got)
	}
}

func TestPost_NilSetPassThrough(t *testing.T) {
	var s *Set
	called := false
	h := s.Post()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil))
	if !called {
		t.Fatal("nil Set.Post() should pass through")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/rules/... -v -run Post_`
Expected: FAIL — `Post` undefined.

- [ ] **Step 3: Implement `Post`**

Create `internal/rules/post.go`:

```go
package rules

import "net/http"

// Post returns the middleware that wraps the ResponseWriter and injects
// matching headers just before the first WriteHeader/Write. Multiple
// matching rules all apply, in declaration order. Headers from rules
// override headers the handler already set (rule wins).
// Safe to call on nil *Set.
func (s *Set) Post() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if s == nil || len(s.headers) == 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(&headerInjector{ResponseWriter: w, set: s, urlPath: r.URL.Path}, r)
		})
	}
}

type headerInjector struct {
	http.ResponseWriter
	set     *Set
	urlPath string
	written bool
}

func (h *headerInjector) inject() {
	if h.written {
		return
	}
	h.written = true
	for _, rule := range h.set.headers {
		params, ok := rule.Pattern.Match(h.urlPath)
		if !ok {
			continue
		}
		for _, kv := range rule.Headers {
			h.Header().Set(kv.Key, rule.Pattern.Expand(kv.Value, params))
		}
	}
}

func (h *headerInjector) WriteHeader(code int) {
	h.inject()
	h.ResponseWriter.WriteHeader(code)
}

func (h *headerInjector) Write(b []byte) (int, error) {
	h.inject()
	return h.ResponseWriter.Write(b)
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/rules/... -v -run Post_`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/rules/post.go internal/rules/post_test.go
git commit -m "feat(rules): Post middleware injects headers from matching rules"
```

---

## Task 10: `internal/rules/listing.go` — listing queries

Pure functions consumed by the handler. `IsHidden(name)` matches `entry.Name()` (no leading `/`). `IsListingEnabled(urlPath)` and `RenderSingle()` consult their respective settings.

**Files:**
- Create: `internal/rules/listing.go`
- Create: `internal/rules/listing_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/rules/listing_test.go`:

```go
package rules

import "testing"

func TestIsListingEnabled_DefaultsTrue(t *testing.T) {
	s := &Set{}
	if !s.IsListingEnabled("/whatever") {
		t.Fatal("default should be true")
	}
}

func TestIsListingEnabled_BooleanFalse(t *testing.T) {
	s := &Set{directoryList: directoryListingValue{hasValue: true, enabled: false}}
	if s.IsListingEnabled("/x") {
		t.Fatal("expected false")
	}
}

func TestIsListingEnabled_PatternMatch(t *testing.T) {
	s := &Set{directoryList: directoryListingValue{
		hasValue: true, enabled: true,
		patterns: []*Pattern{mustCompile(t, "/public/**")},
	}}
	if !s.IsListingEnabled("/public/sub") {
		t.Fatal("/public/sub should be listed")
	}
	if s.IsListingEnabled("/private/x") {
		t.Fatal("/private/x should not be listed (no pattern match)")
	}
}

func TestIsHidden_PatternsMatchFilename(t *testing.T) {
	s := &Set{unlisted: []*Pattern{mustCompile(t, ".git"), mustCompile(t, "*.bak")}}
	if !s.IsHidden(".git") {
		t.Fatal(".git should be hidden")
	}
	if !s.IsHidden("notes.bak") {
		t.Fatal("notes.bak should be hidden")
	}
	if s.IsHidden("index.html") {
		t.Fatal("index.html should be visible")
	}
}

func TestRenderSingle(t *testing.T) {
	if (&Set{}).RenderSingle() {
		t.Fatal("default false")
	}
	if !(&Set{renderSingle: true}).RenderSingle() {
		t.Fatal("expected true")
	}
}

func TestWantsNoTrailingSlash(t *testing.T) {
	if (&Set{}).WantsNoTrailingSlash() {
		t.Fatal("default false when trailingSlash absent")
	}
	tval := true
	if (&Set{trailingSlash: &tval}).WantsNoTrailingSlash() {
		t.Fatal("trailingSlash:true should not opt out of the F1 redirect")
	}
	fval := false
	if !(&Set{trailingSlash: &fval}).WantsNoTrailingSlash() {
		t.Fatal("trailingSlash:false should be true")
	}
}

func TestNilSet_AllQueriesSafe(t *testing.T) {
	var s *Set
	if !s.IsListingEnabled("/x") {
		t.Fatal("nil should default true")
	}
	if s.IsHidden("anything") {
		t.Fatal("nil should not hide anything")
	}
	if s.RenderSingle() {
		t.Fatal("nil should default false")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/rules/... -v -run "IsListingEnabled|IsHidden|RenderSingle|NilSet"`
Expected: FAIL — query methods undefined.

- [ ] **Step 3: Implement `listing.go`**

Create `internal/rules/listing.go`:

```go
package rules

// IsListingEnabled reports whether a directory at urlPath should produce
// an HTML listing. Defaults to true when the rule is absent.
func (s *Set) IsListingEnabled(urlPath string) bool {
	if s == nil || !s.directoryList.hasValue {
		return true
	}
	if !s.directoryList.enabled {
		return false
	}
	if len(s.directoryList.patterns) == 0 {
		return true
	}
	for _, p := range s.directoryList.patterns {
		if _, ok := p.Match(urlPath); ok {
			return true
		}
	}
	return false
}

// IsHidden reports whether a directory entry with the given filename
// should be omitted from a listing.
func (s *Set) IsHidden(name string) bool {
	if s == nil {
		return false
	}
	for _, p := range s.unlisted {
		if _, ok := p.Match(name); ok {
			return true
		}
	}
	return false
}

// RenderSingle reports whether dirs with exactly one non-hidden file
// should serve that file directly.
func (s *Set) RenderSingle() bool {
	if s == nil {
		return false
	}
	return s.renderSingle
}

// WantsNoTrailingSlash reports whether the rule set explicitly opts out of
// the handler's F1 "redirect /dir to /dir/" behavior. Returns true iff
// trailingSlash is set and is false. Consumed by the handler so that the
// F1 default redirect doesn't fight a trailingSlash:false strip in Pre.
func (s *Set) WantsNoTrailingSlash() bool {
	if s == nil || s.trailingSlash == nil {
		return false
	}
	return !*s.trailingSlash
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/rules/... -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/rules/listing.go internal/rules/listing_test.go
git commit -m "feat(rules): IsListingEnabled / IsHidden / RenderSingle queries"
```

---

## Task 11: Refactor `handler.New` to accept `*rules.Set`

Pure signature change. Existing tests pass `nil`. No behavior change yet — the new param is wired in Task 12.

**Files:**
- Modify: `internal/handler/handler.go`
- Modify: `internal/handler/handler_test.go`
- Modify: `cmd/serve/main.go`
- Modify: `cmd/serve/serve_e2e_test.go`

- [ ] **Step 1: Update `handler.New` signature**

In `internal/handler/handler.go`:

```go
import (
	// existing imports …
	"serve/internal/rules"
)

// New returns an http.Handler that serves files from fsys according to cfg.
// ruleSet may be nil; if non-nil, its Pre and Post middlewares are mounted.
func New(cfg config.Config, fsys fs.FS, ruleSet *rules.Set) http.Handler {
	core := &core{cfg: cfg, fsys: fsys, ruleSet: ruleSet}
	var h http.Handler = http.HandlerFunc(core.serve)
	h = ruleSet.Post()(h)
	h = ruleSet.Pre()(h)
	h = corsMiddleware(cfg.CORS)(h)
	return h
}

type core struct {
	cfg     config.Config
	fsys    fs.FS
	ruleSet *rules.Set
}
```

The `Post()` is mounted closest to the core (wraps the writer for one request), `Pre()` outside it (handles redirects/rewrites before reaching core), CORS outermost.

- [ ] **Step 2: Update existing callers**

In `internal/handler/handler_test.go`, change `newHandler`:

```go
func newHandler(cfg config.Config, fsys fstest.MapFS) http.Handler {
	return New(cfg, fsys, nil)
}
```

In `cmd/serve/main.go`:

```go
h := logx.Middleware(log.Default(), cfg.NoRequestLogging)(
	handler.New(cfg, osDirFS(cfg.Directory), nil),
)
```

In `cmd/serve/serve_e2e_test.go`:

```go
h := handler.New(cfg, osDirFS(dir), nil)
```

- [ ] **Step 3: Run all tests and build**

Run: `go test ./... -count=1 && go build ./cmd/serve`
Expected: all green, binary builds.

- [ ] **Step 4: Commit**

```bash
git add internal/handler/handler.go internal/handler/handler_test.go cmd/serve/main.go cmd/serve/serve_e2e_test.go
git commit -m "refactor(handler): New accepts *rules.Set (no behavior change yet)"
```

---

## Task 12: Wire `ruleSet` listing queries + integration tests

Use `ruleSet.IsListingEnabled`, `IsHidden`, `RenderSingle` in the handler. Apply `unlisted` filter inside `serveDirectory`. Add 10 integration tests that exercise every rule type through `handler.New`.

**Files:**
- Modify: `internal/handler/handler.go`
- Modify: `internal/handler/directory.go`
- Modify: `internal/handler/handler_test.go`

- [ ] **Step 1: Write the integration tests**

Append to `internal/handler/handler_test.go`. Each test builds a `*rules.Set` directly (no JSON) so it's self-contained:

```go
import (
	// existing imports …
	"serve/internal/rules"
)

func mustRuleSet(t *testing.T, body string) *rules.Set {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "serve.json"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	s, err := rules.Load("", dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return s
}

func TestRules_RedirectFires(t *testing.T) {
	set := mustRuleSet(t, `{"redirects":[{"source":"/old","destination":"/new"}]}`)
	h := New(config.Config{}, mkFS(), set)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/old", nil))
	if rec.Code != 301 || rec.Header().Get("Location") != "/new" {
		t.Fatalf("got %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestRules_RewriteServesAliasedFile(t *testing.T) {
	set := mustRuleSet(t, `{"rewrites":[{"source":"/api/:id","destination":"/api/:id.json"}]}`)
	fsys := fstest.MapFS{"api/42.json": &fstest.MapFile{Data: []byte(`{"id":42}`)}}
	h := New(config.Config{}, fsys, set)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/42", nil))
	if rec.Code != 200 || rec.Body.String() != `{"id":42}` {
		t.Fatalf("got %d body=%q", rec.Code, rec.Body.String())
	}
}

func TestRules_CleanUrlsServeHTML(t *testing.T) {
	set := mustRuleSet(t, `{"cleanUrls": true}`)
	fsys := fstest.MapFS{"about.html": &fstest.MapFile{Data: []byte("<h1>about</h1>")}}
	// SetExists must close over fsys; the handler does this in cmd/serve. For
	// tests we wire it explicitly:
	set.SetExists(func(p string) bool {
		_, err := fsys.Open(stripLeadingSlash(p))
		return err == nil
	})
	h := New(config.Config{}, fsys, set)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/about", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "about") {
		t.Fatalf("got %d body=%q", rec.Code, rec.Body.String())
	}
}

func TestRules_TrailingSlashStripOverridesF1Default(t *testing.T) {
	set := mustRuleSet(t, `{"trailingSlash": false}`)
	h := New(config.Config{}, mkFS(), set)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/sub/", nil))
	if rec.Code != 301 || rec.Header().Get("Location") != "/sub" {
		t.Fatalf("got %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestRules_HeadersInjected(t *testing.T) {
	set := mustRuleSet(t, `{"headers":[{"source":"/**","headers":[{"key":"Cache-Control","value":"max-age=10"}]}]}`)
	h := New(config.Config{}, mkFS(), set)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/index.html", nil))
	if got := rec.Header().Get("Cache-Control"); got != "max-age=10" {
		t.Fatalf("Cache-Control %q", got)
	}
}

func TestRules_DirectoryListingDisabled404(t *testing.T) {
	set := mustRuleSet(t, `{"directoryListing": false}`)
	h := New(config.Config{}, mkFS(), set)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/docs/", nil))
	if rec.Code != 404 {
		t.Fatalf("status %d, want 404", rec.Code)
	}
}

func TestRules_UnlistedHidesFromListing(t *testing.T) {
	mod := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	fsys := fstest.MapFS{
		"area/visible.txt": &fstest.MapFile{Data: []byte("v"), ModTime: mod},
		"area/secret.txt":  &fstest.MapFile{Data: []byte("s"), ModTime: mod},
	}
	set := mustRuleSet(t, `{"unlisted": ["secret.txt"]}`)
	h := New(config.Config{}, fsys, set)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/area/", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "visible.txt") || strings.Contains(body, "secret.txt") {
		t.Fatalf("listing should hide secret.txt; got:\n%s", body)
	}
}

func TestRules_RenderSingleServesLoneFile(t *testing.T) {
	mod := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	fsys := fstest.MapFS{
		"lone/only.txt": &fstest.MapFile{Data: []byte("hello"), ModTime: mod},
	}
	set := mustRuleSet(t, `{"renderSingle": true}`)
	h := New(config.Config{}, fsys, set)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/lone/", nil))
	if rec.Code != 200 || rec.Body.String() != "hello" {
		t.Fatalf("got %d body=%q", rec.Code, rec.Body.String())
	}
}

func TestRules_NilSetUnchanged(t *testing.T) {
	h := New(config.Config{}, mkFS(), nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/index.html", nil))
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
}

// helper used by TestRules_CleanUrlsServeHTML
func stripLeadingSlash(p string) string {
	if len(p) > 0 && p[0] == '/' {
		return p[1:]
	}
	return p
}
```

- [ ] **Step 2: Run tests and confirm relevant ones fail**

Run: `go test ./internal/handler/... -run TestRules_ -v`
Expected: most FAIL — handler doesn't yet honor `directoryListing`, `unlisted`, `renderSingle`.

- [ ] **Step 3: Wire listing/unlisted/renderSingle into the handler**

In `internal/handler/handler.go`, replace the `info.IsDir()` block with:

```go
if info.IsDir() {
	// F1 default: /dir → 301 /dir/. Suppressed by `trailingSlash: false`
	// to avoid a redirect-strip-vs-add loop with the Pre stage.
	if !strings.HasSuffix(urlPath, "/") && !c.ruleSet.WantsNoTrailingSlash() {
		http.Redirect(w, r, urlPath+"/", http.StatusMovedPermanently)
		return
	}
	indexPath := pathJoin(cleaned, "index.html")
	if idx, err := fs.Stat(c.fsys, indexPath); err == nil && !idx.IsDir() {
		c.serveFile(w, r, indexPath)
		return
	}
	// renderSingle: dir with exactly one non-hidden file → serve it directly
	if c.ruleSet.RenderSingle() {
		if only, ok := singleVisibleFile(c.fsys, cleaned, c.ruleSet); ok {
			c.serveFile(w, r, only)
			return
		}
	}
	if !c.ruleSet.IsListingEnabled(urlPath) {
		http.NotFound(w, r)
		return
	}
	if err := serveDirectory(w, r, c.fsys, cleaned, urlPath, c.ruleSet); err != nil {
		http.Error(w, "Unable to read directory", http.StatusInternalServerError)
	}
	return
}
```

Add `singleVisibleFile`:

```go
import (
	// existing imports plus:
	"serve/internal/rules"
)

func singleVisibleFile(fsys fs.FS, dir string, set *rules.Set) (string, bool) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return "", false
	}
	var only string
	count := 0
	for _, e := range entries {
		if set.IsHidden(e.Name()) {
			continue
		}
		if e.IsDir() {
			return "", false
		}
		count++
		only = pathJoin(dir, e.Name())
		if count > 1 {
			return "", false
		}
	}
	if count != 1 {
		return "", false
	}
	return only, true
}
```

In `internal/handler/directory.go`, accept `*rules.Set` and filter entries:

```go
import (
	// existing imports plus:
	"serve/internal/rules"
)

func serveDirectory(w http.ResponseWriter, _ *http.Request, fsys fs.FS, dirPath, urlPath string, set *rules.Set) error {
	entries, err := fs.ReadDir(fsys, dirPath)
	if err != nil {
		return err
	}
	if set != nil {
		filtered := entries[:0]
		for _, e := range entries {
			if !set.IsHidden(e.Name()) {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}
	// rest unchanged …
}
```

Make sure `directory_test.go`'s call site passes `nil` for the new param.

- [ ] **Step 4: Run tests and verify they pass**

Run: `go test ./... -count=1`
Expected: all green, including the 9 new `TestRules_*` tests.

- [ ] **Step 5: Commit**

```bash
git add internal/handler/handler.go internal/handler/directory.go internal/handler/handler_test.go internal/handler/directory_test.go
git commit -m "feat(handler): consult *rules.Set for listing/unlisted/renderSingle + integration tests"
```

---

## Task 13: Wire `cmd/serve/main.go` to load rules and merge into config

The full integration: load `serve.json`, install `SetExists`, merge `public`/`symlinks` into `cfg`, pass `*Set` to `handler.New`.

**Files:**
- Modify: `cmd/serve/main.go`

- [ ] **Step 1: Update `main.go`**

```go
import (
	// existing imports plus:
	"serve/internal/rules"
)

func main() {
	cfg, cliSet, err := config.ParseFlags(os.Args)
	// ... existing help/version/directory resolution ...

	ruleSet, err := rules.Load(cfg.ConfigFile, cfg.Directory)
	if err != nil {
		log.Fatalf("serve.json: %v", err)
	}
	ruleSet.MergeIntoConfig(&cfg, cliSet)

	// Re-stat the directory in case `public` changed it.
	if _, err := os.Stat(cfg.Directory); err != nil {
		log.Fatalf("directory %q: %v", cfg.Directory, err)
	}

	fsys := osDirFS(cfg.Directory)
	ruleSet.SetExists(func(urlPath string) bool {
		p := strings.TrimPrefix(urlPath, "/")
		if p == "" {
			return false
		}
		_, statErr := fs.Stat(fsys, p)
		return statErr == nil
	})

	// ... existing listener.Build, etc., unchanged ...

	h := logx.Middleware(log.Default(), cfg.NoRequestLogging)(
		handler.New(cfg, fsys, ruleSet),
	)
	// ... rest unchanged ...
}
```

- [ ] **Step 2: Build and run existing E2E test**

Run: `go build ./cmd/serve && go test ./cmd/serve/... -count=1`
Expected: PASS (E2E with no `serve.json` should still work).

- [ ] **Step 3: Commit**

```bash
git add cmd/serve/main.go
git commit -m "feat(cmd): load serve.json, merge into config, wire SetExists"
```

---

## Task 14: Golden fixtures driver + 4 fixtures

A table-driven test that walks `internal/rules/testdata/*/`, loads each fixture's `serve.json`, runs each request from its `requests.json`, and asserts the response.

**Files:**
- Create: `internal/rules/golden_test.go`
- Create: `internal/rules/testdata/<name>/serve.json`, `files/...`, `requests.json` (×4)

- [ ] **Step 1: Write the driver test**

Create `internal/rules/golden_test.go`:

```go
package rules_test

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"serve/internal/config"
	"serve/internal/handler"
	"serve/internal/rules"
)

type goldenRequest struct {
	Method       string            `json:"method"`
	URL          string            `json:"url"`
	Accept       string            `json:"accept,omitempty"`
	Status       int               `json:"status"`
	Headers      map[string]string `json:"headers,omitempty"`
	BodyContains string            `json:"bodyContains,omitempty"`
}

func TestGoldenFixtures(t *testing.T) {
	fixtures, err := os.ReadDir("testdata")
	if err != nil {
		t.Skipf("no testdata: %v", err)
	}
	for _, fx := range fixtures {
		if !fx.IsDir() {
			continue
		}
		t.Run(fx.Name(), func(t *testing.T) {
			runGolden(t, filepath.Join("testdata", fx.Name()))
		})
	}
}

func runGolden(t *testing.T, dir string) {
	t.Helper()
	fsys := buildFS(t, filepath.Join(dir, "files"))
	set, err := rules.Load(filepath.Join(dir, "serve.json"), "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	set.SetExists(func(p string) bool {
		s := strings.TrimPrefix(p, "/")
		if s == "" {
			return false
		}
		_, err := fsys.Open(s)
		return err == nil
	})
	h := handler.New(config.Config{}, fsys, set)

	data, err := os.ReadFile(filepath.Join(dir, "requests.json"))
	if err != nil {
		t.Fatalf("requests.json: %v", err)
	}
	var reqs []goldenRequest
	if err := json.Unmarshal(data, &reqs); err != nil {
		t.Fatalf("parse requests.json: %v", err)
	}

	for i, gr := range reqs {
		t.Run(gr.URL, func(t *testing.T) {
			method := gr.Method
			if method == "" {
				method = "GET"
			}
			req := httptest.NewRequest(method, gr.URL, nil)
			if gr.Accept != "" {
				req.Header.Set("Accept", gr.Accept)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != gr.Status {
				t.Fatalf("[req %d] status %d, want %d", i, rec.Code, gr.Status)
			}
			for k, v := range gr.Headers {
				if got := rec.Header().Get(k); got != v {
					t.Fatalf("[req %d] header %s: %q, want %q", i, k, got, v)
				}
			}
			if gr.BodyContains != "" && !strings.Contains(rec.Body.String(), gr.BodyContains) {
				t.Fatalf("[req %d] body should contain %q, got:\n%s", i, gr.BodyContains, rec.Body.String())
			}
		})
	}
}

func buildFS(t *testing.T, root string) fstest.MapFS {
	t.Helper()
	mod := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	fsys := fstest.MapFS{}
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		fsys[rel] = &fstest.MapFile{Data: data, ModTime: mod}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	return fsys
}
```

- [ ] **Step 2: Create fixture 1 — SPA with redirects**

```
internal/rules/testdata/spa-with-redirects/
  serve.json
  files/
    index.html
    about.html
    api/users.json
  requests.json
```

`serve.json`:
```json
{
  "cleanUrls": true,
  "redirects": [
    {"source": "/legacy/:slug", "destination": "/blog/:slug"}
  ],
  "rewrites": [
    {"source": "/api/**", "destination": "/api/users.json"}
  ]
}
```

`files/index.html`: `<h1>Home</h1>`
`files/about.html`: `<h1>About</h1>`
`files/api/users.json`: `[{"id":1,"name":"Alice"}]`

`requests.json`:
```json
[
  {"url": "/", "status": 200, "bodyContains": "Home"},
  {"url": "/about", "status": 200, "bodyContains": "About"},
  {"url": "/about.html", "status": 301, "headers": {"Location": "/about"}},
  {"url": "/legacy/hello", "status": 301, "headers": {"Location": "/blog/hello"}},
  {"url": "/api/anything", "status": 200, "bodyContains": "Alice"}
]
```

- [ ] **Step 3: Create fixture 2 — headers + caching policy**

```
internal/rules/testdata/static-with-caching/
  serve.json
  files/{index.html, app.js, style.css}
  requests.json
```

`serve.json`:
```json
{
  "headers": [
    {"source": "**/*.css", "headers": [{"key": "Cache-Control", "value": "public, max-age=31536000"}]},
    {"source": "**/*.js",  "headers": [{"key": "Cache-Control", "value": "public, max-age=31536000"}]},
    {"source": "**/*.html","headers": [{"key": "Cache-Control", "value": "no-cache"}]}
  ]
}
```

Files: any content; `requests.json` asserts each Cache-Control value.

- [ ] **Step 4: Create fixture 3 — trailingSlash strip + directoryListing array**

```
internal/rules/testdata/strict-trailing/
  serve.json
  files/{public/index.html, private/secret.txt}
  requests.json
```

`serve.json`:
```json
{
  "trailingSlash": false,
  "directoryListing": ["/public**"]
}
```

The pattern `/public**` compiles to `^/public.*$`, matching `/public`, `/public/`, and `/public/anything` — so the listing toggle works regardless of whether the request URL has a trailing slash.

`requests.json` asserts: `/public/` returns 301 with `Location: /public`, then `/public` (after follow) serves `index.html` (200), and `/private/` returns 404 (pattern doesn't match `/private...`).

- [ ] **Step 5: Create fixture 4 — renderSingle + unlisted**

```
internal/rules/testdata/render-single/
  serve.json
  files/
    docs/.git/HEAD
    docs/only.md
  requests.json
```

`serve.json`:
```json
{
  "renderSingle": true,
  "unlisted": [".git"]
}
```

`files/docs/only.md`: `# Only file`
`files/docs/.git/HEAD`: `ref: refs/heads/main`

`requests.json`:
```json
[
  {"url": "/docs/", "status": 200, "bodyContains": "Only file"}
]
```

- [ ] **Step 6: Run tests and verify all 4 fixtures pass**

Run: `go test ./internal/rules/... -run TestGoldenFixtures -v`
Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/rules/golden_test.go internal/rules/testdata
git commit -m "test(rules): golden fixtures driver + 4 real-world serve.json scenarios"
```

---

## Task 15: Extend `cmd/serve/serve_e2e_test.go` with a `serve.json` case

E2E proves that a binary started with `-c serve.json` honors a rule end-to-end.

**Files:**
- Modify: `cmd/serve/serve_e2e_test.go`

- [ ] **Step 1: Append the new test**

```go
func TestE2E_ServeJsonRedirect(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("home"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "serve.json"),
		[]byte(`{"redirects":[{"source":"/old","destination":"/new"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	lns, err := listener.Build([]string{":0"}, true)
	if err != nil {
		t.Fatalf("listener: %v", err)
	}
	defer func() { for _, l := range lns { l.Close() } }()

	set, err := rules.Load("", dir)
	if err != nil {
		t.Fatalf("rules.Load: %v", err)
	}
	cfg := config.Config{Directory: dir, NoRequestLogging: true}
	h := handler.New(cfg, osDirFS(dir), set)

	srv := &http.Server{Handler: h}
	go func() { _ = srv.Serve(lns[0]) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	// Use a non-following client so we observe the 301 directly.
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get("http://" + lns[0].Addr().String() + "/old")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 301 {
		t.Fatalf("status %d, want 301", resp.StatusCode)
	}
	if resp.Header.Get("Location") != "/new" {
		t.Fatalf("Location %q", resp.Header.Get("Location"))
	}
}
```

Make sure `serve/internal/rules` is imported.

- [ ] **Step 2: Run the test and verify it passes**

Run: `go test ./cmd/serve/... -count=1 -run TestE2E -v`
Expected: both E2E tests PASS.

- [ ] **Step 3: Commit**

```bash
git add cmd/serve/serve_e2e_test.go
git commit -m "test(cmd): E2E asserting serve.json redirect through the wired binary"
```

---

## Task 16: README update with `serve.json` section + example

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Insert a `## serve.json` section after `## Flags`**

Add this content:

```markdown
## serve.json

If a `serve.json` file exists in the served directory (or you pass `-c <path>`), `serve` reads it for behavior overrides. The schema mirrors npm `serve-handler`:

```json
{
  "public": "./dist",
  "cleanUrls": true,
  "trailingSlash": false,
  "renderSingle": false,
  "directoryListing": true,
  "unlisted": [".git", "*.bak"],
  "redirects": [
    {"source": "/legacy/:slug", "destination": "/blog/:slug", "type": 301}
  ],
  "rewrites": [
    {"source": "/api/:version/users", "destination": "/data/users-:version.json"}
  ],
  "headers": [
    {"source": "**/*.css", "headers": [{"key": "Cache-Control", "value": "public, max-age=31536000"}]}
  ],
  "symlinks": false
}
```

**Pattern syntax** (`path-to-regexp` v6 subset): `:name`, `:name?`, `:name+`, `:name*`, `*`, `**`. Captures can be referenced in destinations with `:name` or `$N` (positional).

**Precedence:** CLI flags > `serve.json` > defaults. For example, passing `-d ./other` overrides `"public": "./dist"` in the config.

**Legacy:** `now.json` (the predecessor of `serve.json`) is also recognized when `serve.json` is absent.

A request with `Range:` is served identity-encoded even when a rule sets `Content-Encoding`.
```

- [ ] **Step 2: Commit**

```bash
git add README.md
git commit -m "docs: README section for serve.json (parity with npm serve)"
```

---

## Phase 2 — Done criteria

After Task 16, verify in the worktree:

- [ ] `go vet ./...` clean
- [ ] `go test ./... -count=1` all green locally; CI matrix passes `-race` on Linux/macOS/Windows
- [ ] All 4 golden fixtures pass (`go test ./internal/rules/... -run TestGoldenFixtures`)
- [ ] Manual spot-check: pick one real-world `serve.json` from a public GitHub project, run `./serve -c /path/to/its/serve.json` against its files, verify the documented routes match npm `serve` behavior (curl)
- [ ] Spec doc unchanged; plan doc (this file) committed in the worktree
- [ ] Branch ready to squash-merge into `main` via `superpowers:finishing-a-development-branch`
