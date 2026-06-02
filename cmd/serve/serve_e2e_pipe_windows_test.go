//go:build windows

package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	winio "github.com/Microsoft/go-winio"

	"serve/internal/config"
	"serve/internal/handler"
	"serve/internal/listener"
)

func TestE2E_NamedPipe(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("pipe-hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	addr := fmt.Sprintf(`\\.\pipe\serve-e2e-%d`, os.Getpid())

	lns, err := listener.Build([]string{"pipe:" + addr}, true, nil)
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
			return winio.DialPipeContext(ctx, addr)
		},
	}}
	resp, err := client.Get("http://pipe/")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "pipe-hi" {
		t.Fatalf("body: %q", body)
	}
}
