package syncclient

import "testing"

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
