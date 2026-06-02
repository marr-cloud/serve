package listener

import (
	"strings"
	"testing"
)

func TestBuild_BindsEphemeralPort(t *testing.T) {
	lns, err := Build([]string{":0"}, true, nil)
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
	// `unix:` and `pipe:` are real schemes in F3; use a scheme nobody knows.
	_, err := Build([]string{"garbage://x"}, true, nil)
	if err == nil {
		t.Fatal("expected error for unknown scheme")
	}
}
