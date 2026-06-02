package rules

import "net/http"

// Post returns the middleware that wraps the ResponseWriter and injects
// matching headers just before the first WriteHeader/Write. Multiple
// matching rules all apply, in declaration order. Headers from rules
// override headers the handler already set (rule wins because it writes last).
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
