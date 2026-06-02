package listener

import (
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
)

// decryptLegacy isolates the deprecated x509.DecryptPEMBlock call so the
// staticcheck directive applies narrowly.
//
//nolint:staticcheck // x509.DecryptPEMBlock is the only stdlib decrypt path for PKCS#1.
func decryptLegacy(block *pem.Block, passphrase []byte) ([]byte, error) {
	return x509.DecryptPEMBlock(block, passphrase)
}

// encryptPEMBlockLegacy is used by tests to produce an encrypted PKCS#1 PEM
// block that the production decrypt path can verify against.
//
//nolint:staticcheck // x509.EncryptPEMBlock matches the deprecated decrypt path.
func encryptPEMBlockLegacy(passphrase, der []byte) (*pem.Block, error) {
	return x509.EncryptPEMBlock(rand.Reader, "RSA PRIVATE KEY", der, passphrase, x509.PEMCipherAES256)
}
