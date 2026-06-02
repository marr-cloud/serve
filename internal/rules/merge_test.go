package rules

import (
	"testing"

	"serve/internal/config"
)

func TestMergeIntoConfig_PublicAppliedWhenNoPositional(t *testing.T) {
	s := &Set{publicDir: "./dist", publicSet: true}
	cfg := &config.Config{}
	s.MergeIntoConfig(cfg, map[string]bool{})
	if cfg.Directory != "./dist" {
		t.Fatalf("Directory %q want ./dist", cfg.Directory)
	}
}

func TestMergeIntoConfig_PublicIgnoredWhenPositionalGiven(t *testing.T) {
	s := &Set{publicDir: "./dist", publicSet: true}
	cfg := &config.Config{Directory: "./from-cli"}
	s.MergeIntoConfig(cfg, map[string]bool{})
	if cfg.Directory != "./from-cli" {
		t.Fatalf("Directory %q want ./from-cli (positional wins)", cfg.Directory)
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
	s.MergeIntoConfig(cfg, map[string]bool{"S": true})
	if cfg.Symlinks {
		t.Fatal("CLI -S=false should win over serve.json true")
	}
}

func TestMergeIntoConfig_SymlinksLongFlagAlsoCountsAsCLI(t *testing.T) {
	s := &Set{symlinks: true, symlinksSet: true}
	cfg := &config.Config{Symlinks: false}
	s.MergeIntoConfig(cfg, map[string]bool{"symlinks": true})
	if cfg.Symlinks {
		t.Fatal("CLI --symlinks=false should win over serve.json true")
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
