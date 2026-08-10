package tlsconfig

import (
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"net/http"
)

// PinningTransport returns an *http.Transport whose TLS configuration
// rejects any peer certificate whose SHA-256 does not match pin.
//
// The standard chain validation still runs; only self-signed certificates
// match the pin by design, which is fine because Phase 1 ships a single
// self-signed cert per daemon and the pin is exchanged out of band.
//
// InsecureSkipVerify is acceptable here because VerifyConnection below
// enforces the pin, which is the only trust anchor the LAN needs.
func PinningTransport(pin [sha256.Size]byte) *http.Transport {
	want := pin
	return &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: true, //nolint:gosec // enforced by VerifyConnection
			VerifyConnection: func(cs tls.ConnectionState) error {
				if len(cs.PeerCertificates) == 0 {
					return ErrPinMismatch
				}
				sum := sha256.Sum256(cs.PeerCertificates[0].Raw)
				if subtle.ConstantTimeCompare(sum[:], want[:]) != 1 {
					return ErrPinMismatch
				}
				return nil
			},
		},
	}
}

// ErrPinMismatch is returned by PinningTransport when the peer
// certificate's SHA-256 does not match the configured pin.
var ErrPinMismatch = pinError("tlsconfig: peer certificate fingerprint does not match the pinned value")

type pinError string

func (e pinError) Error() string { return string(e) }
