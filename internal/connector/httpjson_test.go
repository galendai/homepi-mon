package connector

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/galendai/homepi-mon/internal/protocol"
)

func TestGetJSONClassifiesAndRedactsHTTPFailures(t *testing.T) {
	const secret = "sk-test-super-secret"
	cases := []struct {
		status int
		class  protocol.ErrorClass
	}{
		{http.StatusUnauthorized, protocol.ErrAuth},
		{http.StatusForbidden, protocol.ErrAuth},
		{http.StatusTooManyRequests, protocol.ErrRateLimited},
		{http.StatusInternalServerError, protocol.ErrUpstream},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer "+secret {
					t.Error("missing bearer token")
				}
				w.Header().Set("Retry-After", "120")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte("raw-body-" + secret))
			}))
			defer srv.Close()
			var dst map[string]any
			err := GetJSON(context.Background(), JSONRequest{
				URL: srv.URL + "/balance?credential=" + secret, BearerToken: secret,
			}, &dst)
			if Classify(err) != tc.class || StatusCode(err) != tc.status {
				t.Fatalf("class/status = %s/%d, err=%v", Classify(err), StatusCode(err), err)
			}
			if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "raw-body") ||
				strings.Contains(err.Error(), "credential=") {
				t.Fatalf("error leaked request or response data: %v", err)
			}
			if tc.status == http.StatusTooManyRequests && RetryAfter(err) != 120 {
				t.Fatalf("RetryAfter = %d, want 120", RetryAfter(err))
			}
		})
	}
}

func TestGetJSONClassifiesTimeoutWithoutURLDetails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	var dst map[string]any
	err := GetJSON(ctx, JSONRequest{URL: srv.URL + "/slow?secret=query"}, &dst)
	if Classify(err) != protocol.ErrTimeout || strings.Contains(err.Error(), "secret=query") {
		t.Fatalf("timeout error = %v", err)
	}
}

func TestGetJSONRejectsUnsafeOrInvalidResponses(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer other.Close()

	cases := map[string]http.HandlerFunc{
		"cross-host redirect": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Location", other.URL)
			w.WriteHeader(http.StatusFound)
		},
		"non-json": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>not json</html>"))
		},
		"oversize": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":"` + strings.Repeat("x", int(MaxResponseBytes)) + `"}`))
		},
		"trailing": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{} {}`))
		},
	}
	for name, handler := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(handler)
			defer srv.Close()
			var dst map[string]any
			err := GetJSON(context.Background(), JSONRequest{URL: srv.URL}, &dst)
			if err == nil {
				t.Fatal("unsafe response was accepted")
			}
			if name != "cross-host redirect" && Classify(err) != protocol.ErrSchemaChanged {
				t.Fatalf("class = %s, err=%v", Classify(err), err)
			}
		})
	}
}

func TestGetJSONDecodesOneJSONDocument(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		_, _ = w.Write([]byte(`{"amount":12.30}`))
	}))
	defer srv.Close()
	var dst struct {
		Amount any `json:"amount"`
	}
	if err := GetJSON(context.Background(), JSONRequest{URL: srv.URL}, &dst); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(dst.Amount) != "12.30" {
		t.Fatalf("amount = %v", dst.Amount)
	}
}
