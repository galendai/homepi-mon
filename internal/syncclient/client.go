// Package syncclient is the display's client for one homepi-node daemon.
//
// It is deliberately one-way and outbound: the Pi dials the daemon, so no
// inbound port has to be opened on the display device (ADR-004). It holds the
// current snapshot in memory, persists exactly one last-known-good copy and
// reconnects with jittered exponential backoff.
package syncclient

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/snapstore"
)

// Read-deadline bounds for detecting a silently dropped link.
//
// The daemon advertises its heartbeat interval, and we allow three missed
// beats before declaring the stream dead. Until the first heartbeat arrives we
// use InitialReadTimeout, which is comfortably under the PRD 9.1 end-to-end
// staleness target of 90 seconds.
const (
	InitialReadTimeout = 60 * time.Second
	MinReadTimeout     = 15 * time.Second
	MaxReadTimeout     = 90 * time.Second
	DialTimeout        = 30 * time.Second
)

// Backoff bounds for reconnection (PRD 8.1 P1-FR-003).
const (
	MinBackoff = 2 * time.Second
	MaxBackoff = 60 * time.Second
)

// Options configures a Client.
type Options struct {
	// BaseURL is the daemon's base address, e.g. "https://dev-mac.lan:8443".
	BaseURL string
	// DeviceID identifies this display.
	DeviceID string
	// Token is this device's scoped bearer token.
	Token string
	// Binding pins the accepted source node (ADR-014). Required.
	Binding protocol.SourceBinding
	// Store persists the last-known-good snapshot. Required.
	Store *snapstore.Store
	// HTTPClient overrides transport, used to pin a TLS certificate.
	HTTPClient *http.Client
	// ClientVersion is reported in the hello message.
	ClientVersion string
	// Logger receives redacted diagnostics.
	Logger *slog.Logger
	// Now and Rand are injectable for deterministic tests.
	Now  func() time.Time
	Rand func() float64
}

// Client keeps one display in sync with one daemon.
type Client struct {
	opts Options
	log  *slog.Logger
	now  func() time.Time
	rnd  func() float64

	mu        sync.RWMutex
	snapshot  *protocol.MetricSnapshot
	connected bool
	lastSync  time.Time
	retryIn   time.Duration

	changed chan struct{}
}

// New builds a Client and loads any existing last-known-good snapshot, so the
// first frame can be drawn before the network is up (MOD-002 7).
func New(opts Options) (*Client, error) {
	if opts.BaseURL == "" || opts.DeviceID == "" || opts.Token == "" {
		return nil, errors.New("syncclient: BaseURL, DeviceID and Token are required")
	}
	if opts.Store == nil {
		return nil, errors.New("syncclient: Store is required")
	}
	if opts.Binding.NodeID == "" {
		return nil, errors.New("syncclient: Binding.NodeID is required")
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	rnd := opts.Rand
	if rnd == nil {
		src := rand.New(rand.NewSource(time.Now().UnixNano()))
		var mu sync.Mutex
		rnd = func() float64 {
			mu.Lock()
			defer mu.Unlock()
			return src.Float64()
		}
	}

	c := &Client{opts: opts, log: log, now: now, rnd: rnd, changed: make(chan struct{}, 1)}

	snap, err := opts.Store.Load()
	switch {
	case err == nil:
		c.snapshot = snap
		c.lastSync = snap.GeneratedAt
		c.log.Info("loaded last-known-good snapshot", "version", snap.SnapshotVersion)
	case errors.Is(err, snapstore.ErrNoSnapshot):
		c.log.Info("no last-known-good snapshot", "reason", err.Error())
	default:
		// A store that cannot be read must not stop the kiosk from starting.
		c.log.Warn("snapshot store unreadable", "error", err.Error())
	}
	return c, nil
}

// Snapshot returns the current snapshot, or nil when none has been received.
func (c *Client) Snapshot() *protocol.MetricSnapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.snapshot
}

// Connected reports whether a stream is currently established.
func (c *Client) Connected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// RetryIn returns the pending reconnect delay, or 0 when connected.
func (c *Client) RetryIn() time.Duration {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.retryIn
}

// Changed is signalled whenever the visible state changes. It is buffered
// depth one, so a burst of updates collapses into a single redraw
// (MOD-002 6).
func (c *Client) Changed() <-chan struct{} { return c.changed }

func (c *Client) notify() {
	select {
	case c.changed <- struct{}{}:
	default:
	}
}

// Run maintains the connection until ctx is cancelled.
func (c *Client) Run(ctx context.Context) {
	failures := 0
	for {
		healthy, err := c.session(ctx)
		if ctx.Err() != nil {
			return
		}
		failures = nextFailureCount(failures, healthy)

		delay := backoff(failures, c.rnd())
		c.setConnected(false, delay)
		c.log.Warn("stream ended, reconnecting",
			"error", redactURL(err), "retry_in", delay.String())

		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return
		}
	}
}

// session runs one connection attempt to completion.
func (c *Client) session(ctx context.Context) (bool, error) {
	url := strings.TrimSuffix(c.opts.BaseURL, "/") +
		"/v1/devices/" + c.opts.DeviceID + "/stream"

	header := http.Header{}
	header.Set("Authorization", "Bearer "+c.opts.Token)

	dialCtx, cancel := context.WithTimeout(ctx, DialTimeout)
	conn, _, err := websocket.Dial(dialCtx, url, &websocket.DialOptions{
		HTTPClient: c.opts.HTTPClient,
		HTTPHeader: header,
	})
	cancel()
	if err != nil {
		return false, fmt.Errorf("dial: %w", err)
	}
	conn.SetReadLimit(snapstore.MaxBytes)
	defer conn.CloseNow()

	if err := c.sendHello(ctx, conn); err != nil {
		return false, fmt.Errorf("hello: %w", err)
	}

	readTimeout := InitialReadTimeout
	healthy := false
	for {
		readCtx, cancel := context.WithTimeout(ctx, readTimeout)
		_, raw, err := conn.Read(readCtx)
		cancel()
		if err != nil {
			return healthy, fmt.Errorf("read: %w", err)
		}

		env, err := protocol.DecodeEnvelope(raw)
		if err != nil {
			c.log.Warn("undecodable stream message", "error", err.Error())
			continue
		}
		switch env.Type {
		case protocol.MsgSnapshotFull, protocol.MsgSnapshotDelta:
			if c.applySnapshot(env.Payload) {
				healthy = true
				c.setConnected(true, 0)
			}
		case protocol.MsgHeartbeat:
			// The successful read is itself the liveness signal; the payload
			// only tells us how long to wait for the next one.
			var hb protocol.Heartbeat
			if protocol.DecodePayload(env, &hb) == nil && hb.Interval > 0 {
				healthy = true
				readTimeout = clampDuration(3*hb.Interval.D(), MinReadTimeout, MaxReadTimeout)
			}
		case protocol.MsgError:
			var se protocol.StreamError
			if protocol.DecodePayload(env, &se) == nil {
				c.log.Warn("daemon reported an error", "code", se.Code, "message", se.Message)
			}
		default:
			// Unknown message types from a newer daemon are ignored, not fatal.
			c.log.Debug("ignoring unknown message", "type", string(env.Type))
		}
	}
}

func clampDuration(d, lo, hi time.Duration) time.Duration {
	if d < lo {
		return lo
	}
	if d > hi {
		return hi
	}
	return d
}

func (c *Client) sendHello(ctx context.Context, conn *websocket.Conn) error {
	hello := protocol.Hello{
		DeviceID:      c.opts.DeviceID,
		ClientVersion: c.opts.ClientVersion,
		SchemaMajor:   protocol.SchemaMajor,
	}
	if snap := c.Snapshot(); snap != nil {
		hello.LastEpoch = snap.SourceEpoch
		hello.LastSnapshotVersion = snap.SnapshotVersion
	}
	raw, err := protocol.Encode(protocol.MsgHello, c.now().UTC(), hello)
	if err != nil {
		return err
	}
	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return conn.Write(writeCtx, websocket.MessageText, raw)
}

// applySnapshot validates, source-checks and version-checks an incoming
// snapshot before it can replace what is on screen.
func (c *Client) applySnapshot(payload []byte) bool {
	snap, err := protocol.DecodeSnapshot(payload)
	if err != nil {
		c.log.Warn("rejected snapshot", "reason", err.Error())
		return false
	}
	if err := c.opts.Binding.Accept(snap); err != nil {
		// MOD-002 8: a snapshot from another node is never merged.
		c.log.Warn("rejected snapshot from unbound source",
			"source_node", snap.SourceNode, "bound_to", c.opts.Binding.NodeID)
		return false
	}

	c.mu.Lock()
	ok, serr := protocol.Supersedes(c.snapshot, snap)
	if !ok {
		c.mu.Unlock()
		c.log.Debug("dropped superseded snapshot", "reason", serr.Error())
		return true
	}
	c.snapshot = snap
	c.lastSync = c.now()
	c.mu.Unlock()

	if err := c.opts.Store.Save(snap); err != nil {
		// A failed persist must not blank the screen: the in-memory snapshot is
		// already current and the next successful save recovers the file.
		c.log.Warn("could not persist snapshot", "error", err.Error())
	}
	c.notify()
	return true
}

func nextFailureCount(previous int, healthySession bool) int {
	if healthySession {
		return 1
	}
	return previous + 1
}

func (c *Client) setConnected(connected bool, retryIn time.Duration) {
	c.mu.Lock()
	changed := c.connected != connected || c.retryIn != retryIn
	c.connected = connected
	c.retryIn = retryIn
	c.mu.Unlock()
	if changed {
		c.notify()
	}
}

// backoff returns a jittered exponential delay bounded by MinBackoff and
// MaxBackoff.
func backoff(failures int, random float64) time.Duration {
	if failures < 1 {
		failures = 1
	}
	d := MinBackoff
	for i := 1; i < failures && d < MaxBackoff; i++ {
		d *= 2
	}
	if d > MaxBackoff {
		d = MaxBackoff
	}
	// Spread by up to +/-20% so several restarts do not resynchronise.
	spread := (random*2 - 1) * 0.2
	out := time.Duration(float64(d) * (1 + spread))
	if out < MinBackoff/2 {
		out = MinBackoff / 2
	}
	return out
}

// redactURL strips anything after "://" up to the first space from an error
// string, so a token accidentally placed in a URL cannot reach the log.
func redactURL(err error) string {
	if err == nil {
		return ""
	}
	rest := err.Error()
	var redacted strings.Builder
	redacted.Grow(len(rest))
	for {
		i := strings.Index(rest, "://")
		if i < 0 {
			redacted.WriteString(rest)
			return redacted.String()
		}
		redacted.WriteString(rest[:i])
		redacted.WriteString("://[redacted]")
		rest = rest[i+3:]
		end := strings.IndexAny(rest, " \"")
		if end < 0 {
			return redacted.String()
		}
		redacted.WriteString(rest[end : end+1])
		rest = rest[end+1:]
	}
}
