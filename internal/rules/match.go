// Package rules implements serve.json parity: redirects, rewrites, headers,
// cleanUrls, trailingSlash, directoryListing, unlisted, renderSingle, public,
// and symlinks. It exposes a hybrid pipeline (Pre/Post middlewares + listing
// query methods) wired into internal/handler.
package rules

import (
	"fmt"
	"regexp"
	"strings"
)

// Pattern is a compiled path-to-regexp v6 subset pattern.
type Pattern struct {
	re   *regexp.Regexp
	keys []string // ordered named captures
}

// Compile turns a path-to-regexp v6 subset source into a *Pattern.
//
// Supported syntax:
//
//	:name        one segment, named capture
//	:name?       optional segment (consumes preceding "/")
//	:name+       one or more segments
//	:name*       zero or more segments
//	*            wildcard one segment, no capture
//	**           recursive wildcard, no capture
//	(regex)      inline regex (passed through verbatim)
//	anything else: literal (regex-escaped)
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

// Match reports whether urlPath matches and returns named captures. Optional
// `:name?` captures that did not fire yield "" rather than being absent.
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
// Tokens with no matching param pass through as the literal text.
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
