package tlsconfig

import (
	"crypto/sha256"
	"crypto/tls"
	"strings"
	"testing"
)

func TestEnsureCertGeneratesAndPersists(t *testing.T) {
	dir := t.TempDir()
	cert, key, err := EnsureCert(dir, "dev-mac", DefaultValidity)
	if err != nil {
		t.Fatalf("EnsureCert: %v", err)
	}
	if len(cert) == 0 || len(key) == 0 {
		t.Fatal("EnsureCert returned empty cert or key")
	}
	if !strings.Contains(string(cert), "BEGIN CERTIFICATE") {
		t.Errorf("cert is not PEM-encoded:\n%s", cert)
	}
	if !strings.Contains(string(key), "BEGIN EC PRIVATE KEY") &&
		!strings.Contains(string(key), "BEGIN PRIVATE KEY") {
		t.Errorf("key is not PEM-encoded:\n%s", key)
	}

	// Second call must not regenerate.
	cert2, _, err := EnsureCert(dir, "dev-mac", DefaultValidity)
	if err != nil {
		t.Fatalf("EnsureCert second: %v", err)
	}
	if string(cert) != string(cert2) {
		t.Error("EnsureCert regenerated the certificate on a second call")
	}
}

func TestEnsureCertRejectsBadInput(t *testing.T) {
	if _, _, err := EnsureCert("", "cn", 0); err == nil {
		t.Error("EnsureCert accepted empty dir")
	}
	if _, _, err := EnsureCert(t.TempDir(), "", 0); err == nil {
		t.Error("EnsureCert accepted empty CN")
	}
}

func TestFingerprintAndParseRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cert, _, err := EnsureCert(dir, "dev-mac", DefaultValidity)
	if err != nil {
		t.Fatalf("EnsureCert: %v", err)
	}
	// Write the cert out so Fingerprint can re-read it.
	certPath := dir + "/cert.pem"
	if err := writeFile(t, certPath, string(cert)); err != nil {
		t.Fatal(err)
	}
	pin, err := Fingerprint(certPath)
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}
	if !strings.HasPrefix(pin, "sha256:") {
		t.Errorf("pin %q missing sha256: prefix", pin)
	}
	// 32 bytes hex -> 64 chars, plus 31 colons, plus the 7-char prefix.
	want := 7 + 64 + 31
	if len(pin) != want {
		t.Errorf("pin length = %d, want %d", len(pin), want)
	}

	got, err := ParseFingerprint(pin)
	if err != nil {
		t.Fatalf("ParseFingerprint: %v", err)
	}
	if got != mustSha256Of(t, cert) {
		t.Errorf("ParseFingerprint returned wrong bytes")
	}

	// Lower-case input is also accepted.
	lower, err := ParseFingerprint(strings.ToLower(pin))
	if err != nil {
		t.Fatalf("ParseFingerprint lower: %v", err)
	}
	if lower != got {
		t.Error("ParseFingerprint lower-case input differs")
	}

	// A truncated pin must be rejected.
	if _, err := ParseFingerprint("sha256:" + pin[len("sha256:"):len("sha256:")+10]); err == nil {
		t.Error("ParseFingerprint accepted a truncated pin")
	}
	// A garbage pin must be rejected.
	if _, err := ParseFingerprint("not-a-pin"); err == nil {
		t.Error("ParseFingerprint accepted garbage")
	}
}

func TestServerTLSConfigRejectsEmpty(t *testing.T) {
	if _, err := ServerTLSConfig(nil, nil); err == nil {
		t.Error("ServerTLSConfig accepted empty input")
	}
	if _, err := ServerTLSConfig([]byte("not pem"), []byte("not pem")); err == nil {
		t.Error("ServerTLSConfig accepted non-PEM input")
	}
}

func TestServerTLSConfigParsesGeneratedPair(t *testing.T) {
	dir := t.TempDir()
	cert, key, err := EnsureCert(dir, "dev-mac", DefaultValidity)
	if err != nil {
		t.Fatalf("EnsureCert: %v", err)
	}
	cfg, err := ServerTLSConfig(cert, key)
	if err != nil {
		t.Fatalf("ServerTLSConfig: %v", err)
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %x, want %x", cfg.MinVersion, tls.VersionTLS12)
	}
	if len(cfg.Certificates) != 1 {
		t.Errorf("Certificates = %d, want 1", len(cfg.Certificates))
	}
}

func TestPinningTransportRejectsWrongPin(t *testing.T) {
	tr := PinningTransport([32]byte{1, 2, 3})
	if tr.TLSClientConfig == nil {
		t.Fatal("PinningTransport did not configure TLS")
	}
	if tr.TLSClientConfig.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %x, want TLS 1.2", tr.TLSClientConfig.MinVersion)
	}
	vc := tr.TLSClientConfig.VerifyConnection
	if vc == nil {
		t.Fatal("PinningTransport did not install VerifyConnection")
	}
	// No peer certificate: must fail.
	if err := vc(tls.ConnectionState{}); err == nil {
		t.Error("VerifyConnection accepted empty state")
	}
}

func TestPinningTransportAcceptsMatchingPin(t *testing.T) {
	state, leaf := stateWithLeaf(t)
	tr := PinningTransport(leaf)
	if err := tr.TLSClientConfig.VerifyConnection(state); err != nil {
		t.Errorf("VerifyConnection rejected matching pin: %v", err)
	}
}

func TestPinningTransportRejectsMismatchedPin(t *testing.T) {
	state, _ := stateWithLeaf(t)
	wrong := sha256.Sum256([]byte("not-the-cert"))
	tr := PinningTransport(wrong)
	if err := tr.TLSClientConfig.VerifyConnection(state); err == nil {
		t.Error("VerifyConnection accepted a mismatched pin")
	}
}

// helpers
func mustSha256Of(t *testing.T, certPEM []byte) [32]byte {
	t.Helper()
	block := decodeFirstPEM(t, certPEM)
	h := sha256.Sum256(block)
	return h
}

func writeFile(t *testing.T, path, body string) error {
	t.Helper()
	if err := writeFileImpl(path, []byte(body)); err != nil {
		return err
	}
	return nil
}
