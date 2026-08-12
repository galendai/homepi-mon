package syncclient

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// U-S001 (Test-Module-002 U019): a session that received a valid liveness
// message resets the historical reconnect count before recording its outage.
func TestNextFailureCountResetsAfterHealthySession(t *testing.T) {
	if got := nextFailureCount(5, true); got != 1 {
		t.Fatalf("healthy session failure count = %d, want 1", got)
	}
	if got := nextFailureCount(5, false); got != 6 {
		t.Fatalf("unhealthy session failure count = %d, want 6", got)
	}
}

// U031 (Test-Module-002): redacting a dial error must not rescan the
// replacement marker and trap the reconnect goroutine in a busy loop.
func TestRedactURLTerminatesAndRedactsEveryURL(t *testing.T) {
	err := errors.New(`Get "https://192.168.31.107:8443/stream": proxyconnect tcp: dial "http://proxy.local:8080": refused`)
	done := make(chan string, 1)
	go func() {
		done <- redactURL(err)
	}()

	select {
	case got := <-done:
		want := `Get "https://[redacted]": proxyconnect tcp: dial "http://[redacted]": refused`
		if got != want {
			t.Fatalf("redactURL() = %q, want %q", got, want)
		}
		if strings.Contains(got, "192.168.31.107") || strings.Contains(got, "proxy.local") {
			t.Fatalf("redactURL() leaked a URL authority: %q", got)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("redactURL did not return; replacement text was likely rescanned")
	}
}
