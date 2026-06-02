package listener

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writePlainCert(t *testing.T, dir string) (certPath, keyPath string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "serve-test"},
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

func TestLoadTLSConfig_Empty(t *testing.T) {
	cfg, err := LoadTLSConfig("", "", "")
	if err != nil {
		t.Fatalf("LoadTLSConfig: %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil cfg when certPath is empty")
	}
}

func TestLoadTLSConfig_CertButNoKey(t *testing.T) {
	if _, err := LoadTLSConfig("/nowhere/cert.pem", "", ""); err == nil {
		t.Fatal("expected error for --ssl-cert without --ssl-key")
	}
}

func TestLoadTLSConfig_Plain(t *testing.T) {
	cert, key := writePlainCert(t, t.TempDir())
	cfg, err := LoadTLSConfig(cert, key, "")
	if err != nil {
		t.Fatalf("LoadTLSConfig: %v", err)
	}
	if cfg == nil || len(cfg.Certificates) != 1 {
		t.Fatalf("expected 1 cert in config")
	}
	if cfg.MinVersion < tlsVersion12 {
		t.Fatalf("MinVersion %d, want >= TLS 1.2 (0x%x)", cfg.MinVersion, tlsVersion12)
	}
}

// writeEncryptedRSACert writes a self-signed RSA cert + encrypted PKCS#1 key
// using the legacy DEK-Info mechanism (the only stdlib-supported encrypted form).
func writeEncryptedRSACert(t *testing.T, dir, passphrase string) (certPath, keyPath string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("genkey: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "serve-test-enc"},
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
	keyDer := x509.MarshalPKCS1PrivateKey(key)
	encBlock, err := encryptPEMBlockLegacy([]byte(passphrase), keyDer)
	if err != nil {
		t.Fatalf("encrypt key: %v", err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(encBlock), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return certPath, keyPath
}

func TestLoadTLSConfig_Encrypted(t *testing.T) {
	dir := t.TempDir()
	cert, key := writeEncryptedRSACert(t, dir, "swordfish")
	passPath := filepath.Join(dir, "pass.txt")
	if err := os.WriteFile(passPath, []byte("swordfish\n"), 0o600); err != nil {
		t.Fatalf("write pass: %v", err)
	}
	cfg, err := LoadTLSConfig(cert, key, passPath)
	if err != nil {
		t.Fatalf("LoadTLSConfig: %v", err)
	}
	if cfg == nil || len(cfg.Certificates) != 1 {
		t.Fatalf("expected 1 cert")
	}
}

func TestLoadTLSConfig_WrongPassphrase(t *testing.T) {
	dir := t.TempDir()
	cert, key := writeEncryptedRSACert(t, dir, "swordfish")
	passPath := filepath.Join(dir, "pass.txt")
	if err := os.WriteFile(passPath, []byte("wrong"), 0o600); err != nil {
		t.Fatalf("write pass: %v", err)
	}
	if _, err := LoadTLSConfig(cert, key, passPath); err == nil {
		t.Fatal("expected error for wrong passphrase")
	}
}

func TestBuild_TLSWraps(t *testing.T) {
	cert, key := writePlainCert(t, t.TempDir())
	cfg, err := LoadTLSConfig(cert, key, "")
	if err != nil {
		t.Fatalf("LoadTLSConfig: %v", err)
	}
	lns, err := Build([]string{":0"}, true, cfg)
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer func() {
		for _, l := range lns {
			_ = l.Close()
		}
	}()

	// Server side: accept one TLS connection and echo a tiny response.
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := lns[0].Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = io.WriteString(conn, "ok")
	}()

	conn, err := tls.Dial("tcp", lns[0].Addr().String(), &tls.Config{InsecureSkipVerify: true}) //nolint:gosec
	if err != nil {
		t.Fatalf("tls.Dial: %v", err)
	}
	defer conn.Close()
	buf := make([]byte, 2)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != "ok" {
		t.Fatalf("body %q", buf)
	}
	<-done
	_ = net.IPv4zero // keep "net" import in use if Build returned a non-TCP listener
}
