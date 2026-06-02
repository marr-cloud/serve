//go:build unix

package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marr-cloud/serve/internal/config"
	"github.com/marr-cloud/serve/internal/handler"
	"github.com/marr-cloud/serve/internal/listener"
)

func TestE2E_UnixSocket(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("unix-hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(t.TempDir(), "serve.sock")

	lns, err := listener.Build([]string{"unix:" + sock}, true, nil)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer func() {
		for _, l := range lns {
			_ = l.Close()
		}
	}()

	cfg := config.Config{Directory: dir, NoRequestLogging: true}
	h := handler.New(cfg, osDirFS(dir), nil)
	srv := &http.Server{Handler: h}
	go func() { _ = srv.Serve(lns[0]) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	client := &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", sock)
		},
	}}
	resp, err := client.Get("http://unix/")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "unix-hi" {
		t.Fatalf("body: %q", body)
	}
}
