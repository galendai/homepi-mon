package webadmin_test

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
)

func TestOpenCodeGoCanBeConfiguredWithoutExposingItsKey(t *testing.T) {
	srv, url := newTestServer(t)
	base := strings.TrimSuffix(url, "/")
	resp, body := doJSON(t, http.MethodGet, base+"/app.js", "", "", nil)
	if resp.StatusCode != http.StatusOK || !bytes.Contains(body, []byte(`type: "opencode_go"`)) {
		t.Fatalf("provider option missing: status=%d", resp.StatusCode)
	}
	resp, body = doJSON(t, http.MethodPost, base+"/api/draft", srv.CSRFToken(), base, map[string]any{
		"id": "go-main", "new_type": "opencode_go", "account_label": "main",
		"region": "global", "interval": "5m", "stale_after": "15m",
		"candidate_secret": "synthetic-go-key",
	})
	if resp.StatusCode != http.StatusOK || !bytes.Contains(body, []byte("go-main")) || bytes.Contains(body, []byte("synthetic-go-key")) {
		t.Fatalf("draft status=%d body=%s", resp.StatusCode, body)
	}
	resp, body = doJSON(t, http.MethodPut, base+"/api/draft", srv.CSRFToken(), base, map[string]any{
		"id": "go-main", "region": "cn",
	})
	if resp.StatusCode < 400 {
		t.Fatalf("unsupported region accepted: %s", body)
	}
}
