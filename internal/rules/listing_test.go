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
	if s.WantsNoTrailingSlash() {
		t.Fatal("nil should default false")
	}
}
