// Package nodeapi serves the authenticated LAN snapshot and event stream that
// the Raspberry Pi consumes.
//
// Every response is built from the in-memory current state, so no provider
// credential, cookie, authorization header or raw upstream body can reach the
// wire: those values have no field in the protocol types at all.
package nodeapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/galendai/homepi-mon/internal/commandbus"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/state"
)

// DefaultHeartbeat is how often the server pings an idle stream (HL-Spec 6.2).
const DefaultHeartbeat = 20 * time.Second

// Device is one paired display and its scoped token.
type Device struct {
	// ID is the device identifier used in the URL path.
	ID string
	// Token is the shared secret for this device only. A device token grants
	// access to its own snapshot and stream and nothing else (HL-Spec 10).
	Token string
}

// RevocationChecker reports whether a token has been revoked in durable
// state. Returning an error fails authentication closed.
type RevocationChecker func(token string) (bool, error)

// Options configures the server.
type Options struct {
	// Store is the daemon's current state. Required.
	Store *state.Current
	// Devices is the paired device list. It may be empty after the last device
	// is revoked; health endpoints remain available while device APIs reject all.
	Devices []Device
	// Heartbeat overrides the stream ping interval.
	Heartbeat time.Duration
	// Logger receives redacted diagnostics.
	Logger *slog.Logger
	// Now overrides the clock in tests.
	Now func() time.Time
	// PollInterval is how often the stream checks for a new snapshot version.
	PollInterval time.Duration
	// RevokedTokens is the set of SHA-256 hex digests of tokens that the
	// server must reject as if they were unknown. Revoked tokens are
	// answered with the same 401 as a wrong token so the response cannot
	// be used to enumerate paired devices (HL-Spec 10).
	RevokedTokens map[string]struct{}
	// RevocationChecker refreshes revocations created by another process while
	// this server is running.
	RevocationChecker RevocationChecker
	// Commands is the bounded Phase 4 queue. Nil disables command control.
	Commands *commandbus.Bus
	// ControlToken authenticates the local-only command publisher.
	ControlToken string
	// Refresh triggers one immediate scheduler collection for refresh_data.
	Refresh func(context.Context, []string) error
	// ConnectorIDs is the configured allowlist for refresh_data.
	ConnectorIDs []string
}

// Server implements the device-facing HTTP API.
type Server struct {
	store        *state.Current
	devices      map[string]string // device ID -> token
	heartbeat    time.Duration
	poll         time.Duration
	log          *slog.Logger
	now          func() time.Time
	revoked      map[string]struct{} // SHA-256(token) -> struct{}
	checkRevoked RevocationChecker
	commands     *commandbus.Bus
	controlToken string
	refresh      func(context.Context, []string) error
	connectors   map[string]bool

	mu sync.RWMutex
}

// New builds a Server.
func New(opts Options) (*Server, error) {
	if opts.Store == nil {
		return nil, errors.New("nodeapi: Store is required")
	}
	devices := make(map[string]string, len(opts.Devices))
	for _, d := range opts.Devices {
		if d.ID == "" || d.Token == "" {
			return nil, errors.New("nodeapi: every device needs an ID and a token")
		}
		if len(d.Token) < 16 {
			return nil, fmt.Errorf("nodeapi: device %q token is shorter than 16 characters", d.ID)
		}
		devices[d.ID] = d.Token
	}
	hb := opts.Heartbeat
	if hb <= 0 {
		hb = DefaultHeartbeat
	}
	poll := opts.PollInterval
	if poll <= 0 {
		poll = time.Second
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	connectors := make(map[string]bool, len(opts.ConnectorIDs))
	for _, id := range opts.ConnectorIDs {
		connectors[id] = true
	}
	if opts.Commands != nil && len(opts.ControlToken) < 32 {
		return nil, errors.New("nodeapi: Phase 4 control token is shorter than 32 characters")
	}
	return &Server{
		store: opts.Store, devices: devices, heartbeat: hb,
		poll: poll, log: log, now: now,
		revoked:      copyRevoked(opts.RevokedTokens),
		checkRevoked: opts.RevocationChecker,
		commands:     opts.Commands,
		controlToken: opts.ControlToken,
		refresh:      opts.Refresh,
		connectors:   connectors,
	}, nil
}

// copyRevoked makes an internal-only copy of the revocation map so callers
// can mutate their own state after construction without surprising the
// running server.
func copyRevoked(in map[string]struct{}) map[string]struct{} {
	if in == nil {
		return map[string]struct{}{}
	}
	out := make(map[string]struct{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// Handler returns the routed HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/devices/{device}/snapshot", s.handleSnapshot)
	mux.HandleFunc("GET /v1/devices/{device}/stream", s.handleStream)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	if s.commands != nil {
		mux.HandleFunc("POST /v1/control/devices/{device}/commands", s.handlePublishCommand)
		mux.HandleFunc("GET /v1/control/commands/{command}", s.handleCommandStatus)
	}
	return mux
}

// handleHealth is an unauthenticated liveness probe. It exposes only whether
// the process is up and whether it has collected anything yet.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"epoch":   s.store.Epoch(),
		"version": s.store.Version(),
		"data":    s.store.HasData(),
	})
}

func (s *Server) handleSnapshot(w http.ResponseWriter, r *http.Request) {
	device, ok := s.authorize(w, r)
	if !ok {
		return
	}

	snap := s.store.Snapshot()
	raw, err := json.Marshal(snap)
	if err != nil {
		s.log.Error("marshal snapshot", "error", err.Error())
		writeError(w, http.StatusInternalServerError, "internal", "could not build snapshot")
		return
	}

	etag := etagFor(snap)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "no-store")
	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	s.log.Debug("snapshot served", "device", device, "version", snap.SnapshotVersion)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

// authorize checks the bearer token against the device named in the path.
//
// A device may only read its own resources, so the path device and the token
// owner must be the same. Comparison is constant time, and a wrong device and a
// wrong token are answered identically so the response cannot be used to
// enumerate paired devices (HL-Spec 10).
func (s *Server) authorize(w http.ResponseWriter, r *http.Request) (string, bool) {
	device := r.PathValue("device")
	presented := bearerToken(r)

	s.mu.RLock()
	want, known := s.devices[device]
	s.mu.RUnlock()
	revoked, revocationErr := s.tokenRevoked(presented)
	if revocationErr != nil {
		s.log.Error("revocation check failed", "error", revocationErr.Error())
	}

	if !known || presented == "" ||
		revoked || revocationErr != nil ||
		subtle.ConstantTimeCompare([]byte(presented), []byte(want)) != 1 {
		w.Header().Set("WWW-Authenticate", `Bearer realm="homepi"`)
		writeError(w, http.StatusUnauthorized, "unauthorized",
			"device token is missing or invalid")
		return "", false
	}
	return device, true
}

func (s *Server) tokenRevoked(token string) (bool, error) {
	if token == "" {
		return false, nil
	}
	s.mu.RLock()
	_, revoked := s.revoked[hashToken(token)]
	s.mu.RUnlock()
	if revoked || s.checkRevoked == nil {
		return revoked, nil
	}
	return s.checkRevoked(token)
}

// hashToken returns the canonical key used for revocation lookups. It is
// duplicated from deviceacl.HashToken so the hot path here does not pull
// in the persistence package; the algorithm must stay identical.
func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return h[len(prefix):]
	}
	return ""
}

// etagFor derives the entity tag from (epoch, snapshot_version), which is the
// snapshot's identity per HL-Spec 6.1.
//
// Hashing the serialised body would defeat the purpose: generated_at advances
// on every request, so the tag would never match and the display would
// re-download unchanged data on every poll.
func etagFor(snap *protocol.MetricSnapshot) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s/%d", snap.SourceEpoch, snap.SnapshotVersion)))
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeError emits a classified, redacted error. It never echoes request
// headers or upstream text.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, protocol.StreamError{Code: code, Message: message})
}
