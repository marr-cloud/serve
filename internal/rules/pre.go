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

// handleRedirect returns true if a redirect response was written.
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

// handleCleanUrls returns true if it wrote a 301 response. It may also mutate
// r.URL.Path (the internal rewrite case: /about → /about.html when the file
// exists). When no Exists callback is installed, cleanUrls is a no-op.
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

// handleTrailingSlash enforces the trailingSlash policy:
//
//	*s.trailingSlash == false: strip "/dir/" → 301 "/dir"
//	*s.trailingSlash == true : add "/dir" → 301 "/dir/" (only if no extension)
//
// Nil opt-out: when s.trailingSlash is nil, this is a no-op.
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
// Used to avoid redirects like /style.css → /style.css/.
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
