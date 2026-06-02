package rules

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"serve/internal/config"
)

// Set is the parsed, compiled view of a serve.json file. Methods are safe
// on nil *Set; treat nil as "no rules loaded".
type Set struct {
	publicDir     string
	symlinks      bool
	symlinksSet   bool
	publicSet     bool
	cleanUrls     cleanUrlsValue
	trailingSlash *bool
	directoryList directoryListingValue
	renderSingle  bool
	unlisted      []*Pattern
	redirects     []Redirect
	rewrites      []Rewrite
	headers       []HeaderRule
	exists        func(string) bool
}

// Redirect is one parsed entry from "redirects".
type Redirect struct {
	Pattern     *Pattern
	Destination string
	Status      int // default 301; 302/307/308 if "type" is set
}

// Rewrite is one parsed entry from "rewrites".
type Rewrite struct {
	Pattern     *Pattern
	Destination string
}

// HeaderRule applies a set of {key, value} pairs to URLs matching Pattern.
type HeaderRule struct {
	Pattern *Pattern
	Headers []HeaderKV
}

type HeaderKV struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type cleanUrlsValue struct {
	enabled  bool
	patterns []*Pattern // empty + enabled = applies to all paths
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
//
//  1. configFile if non-empty (must exist; missing → error).
//  2. <dir>/serve.json
//  3. <dir>/now.json (legacy alias)
//  4. nothing → returns &Set{} (no-op).
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

// Public returns the override directory from serve.json, "" if unset.
func (s *Set) Public() string {
	if s == nil {
		return ""
	}
	return s.publicDir
}

// Redirects returns the compiled redirect rules.
func (s *Set) Redirects() []Redirect {
	if s == nil {
		return nil
	}
	return s.redirects
}

// Rewrites returns the compiled rewrite rules.
func (s *Set) Rewrites() []Rewrite {
	if s == nil {
		return nil
	}
	return s.rewrites
}

// Headers returns the compiled header rules.
func (s *Set) Headers() []HeaderRule {
	if s == nil {
		return nil
	}
	return s.headers
}

// SetExists installs the filesystem-existence callback used by cleanUrls.
// The callback receives a URL-style absolute path (e.g. "/about.html") and
// should return true iff a file at that path exists in the served fs.
// Pass nil to clear. Safe on nil *Set.
func (s *Set) SetExists(fn func(urlPath string) bool) {
	if s == nil {
		return
	}
	s.exists = fn
}

// appliesTo reports whether cleanUrls is enabled for urlPath. When patterns
// is empty and enabled is true, applies to every path.
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

// MergeIntoConfig applies serve.json overrides for `public` and `symlinks`
// onto cfg, only when the user did NOT set the corresponding CLI value.
// For `public`, the signal is whether cfg.Directory is empty (no positional
// argument was given). For `symlinks`, it is whether -S or --symlinks
// appeared in cliSet. Safe to call on a nil *Set.
func (s *Set) MergeIntoConfig(cfg *config.Config, cliSet map[string]bool) {
	if s == nil || cfg == nil {
		return
	}
	if s.publicSet && cfg.Directory == "" {
		cfg.Directory = s.publicDir
	}
	if s.symlinksSet && !cliSet["S"] && !cliSet["symlinks"] {
		cfg.Symlinks = s.symlinks
	}
}
