package rules

// IsListingEnabled reports whether a directory at urlPath should produce
// an HTML listing. Defaults to true when the directoryListing rule is absent.
// When set to an array of patterns, returns true iff at least one matches.
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

// IsHidden reports whether a directory entry with the given filename should
// be omitted from a listing. Patterns are matched against the bare filename
// (no leading "/" or path components).
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

// RenderSingle reports whether dirs with exactly one non-hidden file should
// serve that file directly instead of generating an HTML listing.
func (s *Set) RenderSingle() bool {
	if s == nil {
		return false
	}
	return s.renderSingle
}

// WantsNoTrailingSlash reports whether the rule set explicitly opts out of
// the handler's F1 "redirect /dir to /dir/" behavior. Returns true iff
// trailingSlash is set and is false. Consumed by the handler to avoid a
// redirect loop with the Pre stage's strip step.
func (s *Set) WantsNoTrailingSlash() bool {
	if s == nil || s.trailingSlash == nil {
		return false
	}
	return !*s.trailingSlash
}
