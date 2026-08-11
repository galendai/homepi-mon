package connector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/galendai/homepi-mon/internal/protocol"
)

// MaxResponseBytes bounds every provider response before decoding. Provider
// payloads are tiny; a larger body is more likely to be an HTML error page or
// a misconfigured endpoint than legitimate usage data.
const MaxResponseBytes int64 = 1 << 20

var errUnsafeRedirect = errors.New("unsafe provider redirect")

// JSONRequest describes one read-only provider request.
type JSONRequest struct {
	Client      *http.Client
	URL         string
	BearerToken string
	Headers     map[string]string
}

// HTTPError preserves only the status code and the safe classified error. It
// deliberately never retains the request URL, response body or headers.
type HTTPError struct {
	Status int
	Err    *Error
}

func (e *HTTPError) Error() string { return e.Err.Error() }
func (e *HTTPError) Unwrap() error { return e.Err }

// StatusCode returns an HTTP response code attached to err, or zero when the
// request failed before a response was received.
func StatusCode(err error) int {
	var he *HTTPError
	if errors.As(err, &he) {
		return he.Status
	}
	return 0
}

// GetJSON performs one GET and decodes exactly one JSON value into dst.
// Redirects may stay on the same scheme and host only, and are capped at three.
func GetJSON(ctx context.Context, opts JSONRequest, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, opts.URL, nil)
	if err != nil {
		return Errorf(protocol.ErrInvalidConfig, "provider URL is invalid")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "homepi-node/phase1")
	if opts.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+opts.BearerToken)
	}
	for key, value := range opts.Headers {
		req.Header.Set(key, value)
	}

	client := safeHTTPClient(opts.Client, req.URL.Scheme, req.URL.Host)
	resp, err := client.Do(req)
	if err != nil {
		switch {
		case errors.Is(err, errUnsafeRedirect):
			return Errorf(protocol.ErrUpstream, "provider redirect rejected")
		case errors.Is(err, context.DeadlineExceeded), errors.Is(ctx.Err(), context.DeadlineExceeded):
			return Errorf(protocol.ErrTimeout, "provider request timed out")
		case errors.Is(err, context.Canceled), errors.Is(ctx.Err(), context.Canceled):
			return Errorf(protocol.ErrTimeout, "provider request canceled")
		default:
			return Errorf(protocol.ErrNetwork, "provider request failed")
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return classifyHTTPStatus(resp)
	}
	mediaType, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || (mediaType != "application/json" && !strings.HasSuffix(mediaType, "+json")) {
		return Errorf(protocol.ErrSchemaChanged, "provider response is not JSON")
	}

	limited := io.LimitReader(resp.Body, MaxResponseBytes+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return Errorf(protocol.ErrNetwork, "provider response could not be read")
	}
	if int64(len(raw)) > MaxResponseBytes {
		return Errorf(protocol.ErrSchemaChanged, "provider response exceeds size limit")
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(dst); err != nil {
		return Errorf(protocol.ErrSchemaChanged, "provider response schema changed")
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		return Errorf(protocol.ErrSchemaChanged, "provider response contains trailing data")
	}
	return nil
}

func safeHTTPClient(base *http.Client, scheme, authority string) *http.Client {
	if base == nil {
		base = &http.Client{Timeout: 30 * time.Second}
	}
	copyClient := *base
	copyClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 || !strings.EqualFold(req.URL.Scheme, scheme) ||
			!strings.EqualFold(req.URL.Host, authority) {
			return errUnsafeRedirect
		}
		return nil
	}
	if copyClient.Timeout <= 0 {
		copyClient.Timeout = 30 * time.Second
	}
	return &copyClient
}

func classifyHTTPStatus(resp *http.Response) error {
	status := resp.StatusCode
	classified := &Error{Class: protocol.ErrUpstream, Msg: fmt.Sprintf("provider returned HTTP %d", status)}
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		classified.Class = protocol.ErrAuth
		classified.Msg = "authentication rejected; update the credential with the official provider tool"
	case http.StatusTooManyRequests:
		classified.Class = protocol.ErrRateLimited
		classified.Msg = "provider rate limit reached"
		classified.RetryAfterSeconds = parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
	}
	return &HTTPError{Status: status, Err: classified}
}

func parseRetryAfter(raw string, now time.Time) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(raw); err == nil && seconds > 0 {
		return seconds
	}
	when, err := http.ParseTime(raw)
	if err != nil || !when.After(now) {
		return 0
	}
	seconds := int(when.Sub(now).Seconds())
	if seconds < 1 {
		return 1
	}
	return seconds
}
