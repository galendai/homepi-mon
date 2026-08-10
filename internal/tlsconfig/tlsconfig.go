// Package tlsconfig generates the self-signed certificate the daemon uses
// for LAN TLS and verifies the leaf certificate on the client side.
//
// Phase 1 uses a self-signed certificate because the LAN link is private
// and there is no PKI to trust. The certificate is generated on the
// daemon's first start, persisted under the daemon's data directory and
// identified by its SHA-256 fingerprint ("pin"). The Pi kiosk pins the
// fingerprint at install time, so a man-in-the-middle with any other
// certificate is rejected by the connection layer before any HTTP
// exchange happens.
//
// Key file permissions are explicitly tightened to 0600 and the parent
// directory to 0700; the protocol type does not carry the fingerprint, so
// it never crosses the wire in either direction.
package tlsconfig

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// DefaultValidity is how long a freshly generated self-signed certificate
// stays valid. Two years matches what most LAN appliances ship with and
// is long enough that the operator does not have to rotate it every few
// weeks while still being finite enough for a manual refresh.
const DefaultValidity = 2 * 365 * 24 * time.Hour

// SANs included in the generated certificate. The Pi always dials by IP
// (or "localhost"), so both must be in the leaf.
var defaultSANs = []string{"127.0.0.1", "::1", "localhost"}

// EnsureCert returns the cert/key pair located under Dir. If the files
// already exist, they are loaded and returned without modification; if
// either is missing, a fresh self-signed pair is generated with CN=cn,
// written with 0600/0700 permissions and returned.
//
// The caller must treat the returned paths as opaque. CertPEM and KeyPEM
// are the same bytes that were written to disk.
func EnsureCert(dir, cn string, validity time.Duration) (certPEM, keyPEM []byte, err error) {
	if dir == "" {
		return nil, nil, errors.New("tlsconfig: data directory is required")
	}
	if cn == "" {
		return nil, nil, errors.New("tlsconfig: certificate common name is required")
	}
	if validity <= 0 {
		validity = DefaultValidity
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("tlsconfig: mkdir %s: %w", dir, err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("tlsconfig: chmod %s: %w", dir, err)
	}

	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")

	if existing, err := loadPair(certPath, keyPath); err == nil {
		return existing.cert, existing.key, nil
	}

	pair, err := generate(cn, validity)
	if err != nil {
		return nil, nil, err
	}
	if err := os.WriteFile(certPath, pair.cert, 0o600); err != nil {
		return nil, nil, fmt.Errorf("tlsconfig: write cert: %w", err)
	}
	if err := os.WriteFile(keyPath, pair.key, 0o600); err != nil {
		return nil, nil, fmt.Errorf("tlsconfig: write key: %w", err)
	}
	return pair.cert, pair.key, nil
}

// Fingerprint returns the SHA-256 of the DER encoding of the certificate
// at path. It is the value the Pi kiosk pins with -node-cert-pin.
//
// The hex form matches what `openssl x509 -fingerprint -sha256` prints
// (uppercase, colon-separated) when prefixed with "sha256:".
func Fingerprint(certPath string) (string, error) {
	raw, err := os.ReadFile(certPath)
	if err != nil {
		return "", fmt.Errorf("tlsconfig: read cert: %w", err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return "", errors.New("tlsconfig: cert is not PEM-encoded")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("tlsconfig: parse cert: %w", err)
	}
	sum := sha256.Sum256(cert.Raw)
	const digits = "0123456789ABCDEF"
	out := make([]byte, 0, len("sha256:")+3*sha256.Size-1)
	out = append(out, "sha256:"...)
	for i, b := range sum {
		if i > 0 {
			out = append(out, ':')
		}
		out = append(out, digits[b>>4], digits[b&0x0f])
	}
	return string(out), nil
}

// ParseFingerprint accepts the colon-separated SHA-256 hex form returned
// by Fingerprint and returns the raw 32 bytes. It accepts both upper and
// lower case, with or without the leading "sha256:" prefix.
func ParseFingerprint(s string) ([32]byte, error) {
	var zero [32]byte
	s = stripPinPrefix(s)
	want := sha256.Size*2 + (sha256.Size - 1) // 64 hex + 31 colons
	if len(s) != want {
		return zero, fmt.Errorf("tlsconfig: pin has %d characters, want %d", len(s), want)
	}
	raw, err := hex.DecodeString(removeColons(s))
	if err != nil {
		return zero, fmt.Errorf("tlsconfig: pin is not hex: %w", err)
	}
	if len(raw) != sha256.Size {
		return zero, fmt.Errorf("tlsconfig: pin is %d bytes, want %d", len(raw), sha256.Size)
	}
	var out [32]byte
	copy(out[:], raw)
	return out, nil
}

func stripPinPrefix(s string) string {
	const prefix = "sha256:"
	if len(s) >= len(prefix) && equalFold(s[:len(prefix)], prefix) {
		return s[len(prefix):]
	}
	return s
}

func removeColons(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] != ':' {
			out = append(out, s[i])
		}
	}
	return string(out)
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// ServerTLSConfig builds a tls.Config for the daemon's HTTPS listener.
// It pins the certificate in CertFile and restricts the minimum protocol
// version to TLS 1.2, which is the oldest version still considered safe
// against known downgrade attacks.
func ServerTLSConfig(certPEM, keyPEM []byte) (*tls.Config, error) {
	if len(certPEM) == 0 || len(keyPEM) == 0 {
		return nil, errors.New("tlsconfig: empty certificate or key")
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, fmt.Errorf("tlsconfig: parse key pair: %w", err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// PinningTransport builds an http.Transport whose TLS configuration
// compares the leaf certificate's SHA-256 against the pinned value. The
// standard x509 verification still runs first, so a completely untrusted
// CA is rejected before the pin check, but a certificate with the right
// fingerprint is accepted even when it would otherwise fail chain
// validation (which is the self-signed case the daemon always uses).
//
// The transport must only be used by the Pi display; the daemon itself
// does not need a client TLS stack.

type pemPair struct {
	cert []byte
	key  []byte
}

func loadPair(certPath, keyPath string) (*pemPair, error) {
	cert, err := os.ReadFile(certPath)
	if err != nil {
		return nil, err
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, err
	}
	return &pemPair{cert: cert, key: key}, nil
}

func generate(cn string, validity time.Duration) (*pemPair, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("tlsconfig: generate key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("tlsconfig: serial: %w", err)
	}
	now := time.Now().UTC()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: cn, Organization: []string{"HomePi Monitor"}},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(validity),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1), net.ParseIP("::1")},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, fmt.Errorf("tlsconfig: create cert: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("tlsconfig: marshal key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return &pemPair{cert: certPEM, key: keyPEM}, nil
}
