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
