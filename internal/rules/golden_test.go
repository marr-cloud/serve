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

	"github.com/marr-cloud/serve/internal/config"
	"github.com/marr-cloud/serve/internal/handler"
	"github.com/marr-cloud/serve/internal/rules"
)

type goldenRequest struct {
	Method       string            `json:"method"`
	URL          string            `json:"url"`
	Accept       string            `json:"accept,omitempty"`
	Status       int               `json:"status"`
	Headers      map[string]string `json:"headers,omitempty"`
	BodyContains string            `json:"bodyContains,omitempty"`
}

// TestGoldenFixtures walks internal/rules/testdata/*/ and, for each fixture
// directory, loads its serve.json + builds an fs.FS from files/, then replays
// each request in requests.json against the wired handler.
func TestGoldenFixtures(t *testing.T) {
	entries, err := os.ReadDir("testdata")
	if err != nil {
		t.Skipf("no testdata: %v", err)
	}
	for _, fx := range entries {
		if !fx.IsDir() {
			continue
		}
		fx := fx
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
		name := gr.Method + " " + gr.URL
		if gr.Method == "" {
			name = "GET " + gr.URL
		}
		t.Run(name, func(t *testing.T) {
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

// buildFS walks root into a fstest.MapFS with deterministic ModTimes so ETag
// values stay stable across machines (helpful when extending these fixtures).
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
