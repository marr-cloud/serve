package listener

import (
	"crypto/tls"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
)

// tlsVersion12 is exposed for tests so they can assert MinVersion without
// importing crypto/tls.
const tlsVersion12 = tls.VersionTLS12

// LoadTLSConfig constructs a *tls.Config from cert/key/passphrase paths.
// Returns (nil, nil) when certPath is empty (TLS disabled).
//
// When passphrasePath is non-empty, the key file is treated as an encrypted
// PEM (PKCS#1, "BEGIN RSA PRIVATE KEY" with a "Proc-Type" header) and
// decrypted with the passphrase read from that file (trailing whitespace
// trimmed). PKCS#8-encrypted keys ("BEGIN ENCRYPTED PRIVATE KEY") are NOT
// supported in this version — Go stdlib has no decrypt path for them. Users
// on modern openssl can convert with:
//
//	openssl pkcs8 -in key.pem -traditional -out key.pkcs1.pem
func LoadTLSConfig(certPath, keyPath, passphrasePath string) (*tls.Config, error) {
	if certPath == "" {
		return nil, nil
	}
	if keyPath == "" {
		return nil, fmt.Errorf("--ssl-cert requires --ssl-key")
	}
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("read cert %q: %w", certPath, err)
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("read key %q: %w", keyPath, err)
	}
	if passphrasePath != "" {
		pass, err := os.ReadFile(passphrasePath)
		if err != nil {
			return nil, fmt.Errorf("read passphrase %q: %w", passphrasePath, err)
		}
		keyPEM, err = decryptPKCS1PEM(keyPEM, strings.TrimRight(string(pass), "\r\n \t"))
		if err != nil {
			return nil, err
		}
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("X509KeyPair: %w", err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// decryptPKCS1PEM decodes a PEM block, decrypts it with the passphrase using
// the legacy PKCS#1 mechanism, and re-encodes it as unencrypted PEM. Refuses
// PKCS#8-encrypted keys with a clear error pointing at the conversion recipe
// in the LoadTLSConfig godoc.
func decryptPKCS1PEM(keyPEM []byte, passphrase string) ([]byte, error) {
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return nil, fmt.Errorf("key PEM: no block found")
	}
	if block.Type == "ENCRYPTED PRIVATE KEY" {
		return nil, fmt.Errorf("PKCS#8 encrypted keys are not supported; convert with `openssl pkcs8 -in key.pem -traditional -out key.pkcs1.pem`")
	}
	der, err := decryptLegacy(block, []byte(passphrase))
	if err != nil {
		return nil, fmt.Errorf("decrypt key: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: block.Type, Bytes: der}), nil
}
