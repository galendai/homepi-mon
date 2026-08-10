package tlsconfig

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"os"
	"testing"
)

// decodeFirstPEM returns the DER bytes of the first PEM block in raw.
func decodeFirstPEM(t *testing.T, raw []byte) []byte {
	t.Helper()
	block, _ := pem.Decode(raw)
	if block == nil {
		t.Fatalf("no PEM block found in %d bytes", len(raw))
	}
	return block.Bytes
}

// writeFileImpl is the production-grade writer: 0600 permissions and
// fsync so a sudden power loss never leaves a half-written cert.
func writeFileImpl(path string, body []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(body); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// stateWithLeaf fabricates a tls.ConnectionState with one leaf certificate
// and returns both the state and that leaf's SHA-256. Tests use the
// returned digest as the pin when they want the matching branch, and
// substitute a different value to exercise the mismatched branch.
func stateWithLeaf(t *testing.T) (tls.ConnectionState, [32]byte) {
	t.Helper()
	dir := t.TempDir()
	cert, _, err := EnsureCert(dir, "dev-mac", DefaultValidity)
	if err != nil {
		t.Fatalf("EnsureCert: %v", err)
	}
	parsed, err := x509.ParseCertificate(decodeFirstPEM(t, cert))
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}
	leaf := sha256.Sum256(parsed.Raw)
	return tls.ConnectionState{PeerCertificates: []*x509.Certificate{parsed}}, leaf
}
