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

	"serve/internal/config"
	"serve/internal/handler"
	"serve/internal/listener"
)

func TestE2E_GetHTML(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>hi</h1>"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	lns, err := listener.Build([]string{":0"}, true)
	if err != nil {
		t.Fatalf("build listener: %v", err)
	}
	defer func() {
		for _, l := range lns {
			_ = l.Close()
		}
	}()

	cfg := config.Config{Directory: dir, NoRequestLogging: true}
	h := handler.New(cfg, osDirFS(dir))
	srv := &http.Server{Handler: h}
	go func() { _ = srv.Serve(lns[0]) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	addr := lns[0].Addr().String()
	if _, _, err := net.SplitHostPort(addr); err != nil {
		t.Fatalf("addr: %v", err)
	}

	resp, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "<h1>hi</h1>" {
		t.Fatalf("body: %q", body)
	}
}
