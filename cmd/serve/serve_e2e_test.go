package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marr-cloud/serve/internal/config"
	"github.com/marr-cloud/serve/internal/handler"
	"github.com/marr-cloud/serve/internal/listener"
	"github.com/marr-cloud/serve/internal/rules"
)

func TestE2E_GetHTML(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<h1>hi</h1>"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	lns, err := listener.Build([]string{":0"}, true, nil)
	if err != nil {
		t.Fatalf("build listener: %v", err)
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

func TestE2E_ServeJsonRedirect(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("home"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "serve.json"),
		[]byte(`{"redirects":[{"source":"/old","destination":"/new"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	lns, err := listener.Build([]string{":0"}, true, nil)
	if err != nil {
		t.Fatalf("listener: %v", err)
	}
	defer func() {
		for _, l := range lns {
			_ = l.Close()
		}
	}()

	set, err := rules.Load("", dir)
	if err != nil {
		t.Fatalf("rules.Load: %v", err)
	}
	cfg := config.Config{Directory: dir, NoRequestLogging: true}
	h := handler.New(cfg, osDirFS(dir), set)

	srv := &http.Server{Handler: h}
	go func() { _ = srv.Serve(lns[0]) }()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	// Non-following client so we can observe the 301 directly.
	client := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Get("http://" + lns[0].Addr().String() + "/old")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 301 {
		t.Fatalf("status %d, want 301", resp.StatusCode)
	}
	if resp.Header.Get("Location") != "/new" {
		t.Fatalf("Location %q", resp.Header.Get("Location"))
	}
}

func TestE2E_HTTPS(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("secure-hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	certPath, keyPath := e2eWritePlainCert(t, dir)

	tlsCfg, err := listener.LoadTLSConfig(certPath, keyPath, "")
	if err != nil {
		t.Fatalf("LoadTLSConfig: %v", err)
	}
	lns, err := listener.Build([]string{":0"}, true, tlsCfg)
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
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
	}}
	resp, err := client.Get("https://" + lns[0].Addr().String() + "/")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "secure-hi" {
		t.Fatalf("body: %q", body)
	}
}

func e2eWritePlainCert(t *testing.T, dir string) (certPath, keyPath string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "serve-e2e"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}
	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	keyDer, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDer}), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return certPath, keyPath
}
