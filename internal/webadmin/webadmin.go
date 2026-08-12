// Package webadmin implements the loopback-only Web Admin for the
// homepi-node daemon. The package's only public entry point is
// Server, which is started by the `homepi-node configure` command.
//
// The server is intentionally narrow in scope:
//
//   - Listens on a single random loopback port (127.0.0.1 / ::1).
//   - Hosts the embedded HTML/CSS/JS UI from internal/webadmin/static.
//   - Exposes a small JSON API for the draft / status / apply flow.
//   - Enforces Host, Origin, CSRF, CSP, SameSite and rate limits so a
//     browser tab running on the operator's machine is the only
//     legitimate caller.
//
// The package never imports the connector packages directly; it
// talks to the configtx.Service which already owns the transaction
// semantics. The host/port injection lives in the cmd/homepi-node
// layer so the test suite can spin up a Server bound to a test
// loopback port without touching the global flag plumbing.
package webadmin

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/galendai/homepi-mon/internal/configtx"
	"github.com/galendai/homepi-mon/internal/displaydeploy"
)

//go:embed static/*
var staticFS embed.FS

// Config configures a Server. The only required field is Service; the
// other fields default to safe loopback-only values.
type Config struct {
	// Service is the Config Transaction Service the UI talks to.
	// The server borrows it for the entire lifetime of the Server;
	// callers must keep the Service alive.
	Service *configtx.Service
	// Addr overrides the default random loopback port. The value
	// is parsed for host/port; anything outside 127.0.0.1 / ::1
	// is rejected before the listener is opened.
	Addr string
	// Logger receives security-relevant events. Nil falls back to
	// slog.Default() with the redacting handler attached.
	Logger *slog.Logger
	// IdleTimeout closes the listener after this duration of no
	// activity. Zero means 1 hour.
	IdleTimeout time.Duration
	// OpenBrowser is invoked once after the listener is ready so
	// the CLI can open the operator's default browser. The Server
	// does not block on the call.
	OpenBrowser func(url string)
	// DisplayStatus is a callback returning the current Pi status
	// for the Overview page. The value is rendered server-side and
	// never reaches the API responses; see the API doc.
	DisplayStatus func() (DisplaySnapshot, error)
	// DisplayManager owns the non-secret profile and fixed SSH transaction.
	// Nil keeps the Display page read-only for compatibility.
	DisplayManager interface {
		State(context.Context) (displaydeploy.State, error)
		Edit(context.Context, displaydeploy.Edit) (displaydeploy.State, error)
		Test(context.Context) (displaydeploy.Result, error)
		Apply(context.Context) (displaydeploy.Result, error)
	}
	// RuntimeStatus compares this Web Admin process with the executable
	// configured by the user service manager. It returns only redacted build
	// metadata; executable paths and service-manager output stay in cmd/install.
	RuntimeStatus func(context.Context) RuntimeSnapshot
	// Restart and HealthCheck are the real user-service operations used
	// by Apply. When either is absent, the Apply endpoint is unavailable.
	Restart     func(context.Context) error
	HealthCheck func(context.Context) error
	// Now is injectable for tests; production code uses time.Now.
	Now func() time.Time
	// MaxMutationsPerMinute limits authenticated state-changing requests.
	// Zero selects 120, which is ample for the UI but bounds request floods.
	MaxMutationsPerMinute int
}

// BuildSnapshot is the safe subset of --version output shown in the browser.
type BuildSnapshot struct {
	Version  string `json:"version"`
	Commit   string `json:"commit"`
	Built    string `json:"built,omitempty"`
	Platform string `json:"platform,omitempty"`
}

// RuntimeSnapshot reports whether the Web Admin and installed daemon target
// are aligned without exposing either executable path.
type RuntimeSnapshot struct {
	State            string        `json:"state"`
	Message          string        `json:"message"`
	Admin            BuildSnapshot `json:"admin"`
	Service          BuildSnapshot `json:"service"`
	ServiceInstalled bool          `json:"service_installed"`
	ServiceRunning   bool          `json:"service_running"`
}

// DisplaySnapshot is the redacted state of the connected Pi shown
// on the Overview page. The fields are deliberately minimal; the
// full DisplayStatus API in configtx will replace this struct when
// P2-03 lands the SSH deployer.
type DisplaySnapshot struct {
	Connected      bool
	Theme          string
	LatestSnapshot time.Time
	SourceEpoch    string
	SnapshotVer    uint64
	Note           string
}

// Server is the Web Admin HTTP server. Construct it with New, then
// call Listen to bind the loopback socket, then Run until the
// listener closes.
type Server struct {
	cfg       Config
	api       *apiState
	srv       *http.Server
	ln        net.Listener
	addr      net.Addr
	csrfTok   string
	activity  chan struct{}
	displayMu chan struct{}
	rateMu    sync.Mutex
	rateStart time.Time
	rateCount int
}

// New validates the configuration and returns a Server ready to
// Listen. The Server is not running yet; the listener is opened by
// Listen.
func New(cfg Config) (*Server, error) {
	if cfg.Service == nil {
		return nil, errors.New("webadmin: Config.Service is required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 1 * time.Hour
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.MaxMutationsPerMinute <= 0 {
		cfg.MaxMutationsPerMinute = 120
	}
	tok, err := randomToken(16)
	if err != nil {
		return nil, err
	}
	s := &Server{
		cfg:       cfg,
		api:       newAPI(cfg.Service),
		csrfTok:   tok,
		activity:  make(chan struct{}, 1),
		displayMu: make(chan struct{}, 1),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/draft", s.handleDraft)
	mux.HandleFunc("/api/draft/diff", s.handleDiff)
	mux.HandleFunc("/api/draft/test", s.handleTest)
	mux.HandleFunc("/api/draft/apply", s.handleApply)
	mux.HandleFunc("/api/types", s.handleTypes)
	mux.HandleFunc("/api/healthz", s.handleHealth)
	mux.HandleFunc("/api/bootstrap", s.handleBootstrap)
	mux.HandleFunc("/api/display/profile", s.handleDisplayProfile)
	mux.HandleFunc("/api/display/test", s.handleDisplayTest)
	mux.HandleFunc("/api/display/apply", s.handleDisplayApply)
	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return nil, fmt.Errorf("webadmin: static fs: %w", err)
	}
	mux.Handle("/static/", http.StripPrefix("/static", staticHandler(staticSub)))
	mux.Handle("/", staticHandler(staticSub))
	s.srv = &http.Server{
		Handler:      s.withSecurity(mux),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  90 * time.Second,
	}
	return s, nil
}

// Listen opens the loopback socket. The returned address is what
// the CLI prints to stdout. Re-listening is not supported.
func (s *Server) Listen() (net.Addr, error) {
	bind := s.cfg.Addr
	if bind == "" {
		bind = "127.0.0.1:0"
	}
	host, _, err := net.SplitHostPort(bind)
	if err != nil {
		return nil, fmt.Errorf("webadmin: parse -addr: %w", err)
	}
	if !isLoopbackHost(host) {
		return nil, fmt.Errorf("webadmin: refusing to bind %q; loopback only", host)
	}
	ln, err := net.Listen("tcp", bind)
	if err != nil {
		return nil, err
	}
	s.ln = ln
	s.addr = ln.Addr()
	s.srv.Addr = s.addr.String()
	return s.addr, nil
}

// url returns the operator-facing URL for the bound listener. The
// caller is responsible for passing it to OpenBrowser if one is set.
func (s *Server) url() string {
	if s.addr == nil {
		return ""
	}
	return "http://" + s.addr.String() + "/"
}

// URL returns the bound URL after Listen. The empty string is
// returned before Listen; tests use it to discover the random port
// without scanning the loopback range.
func (s *Server) URL() string { return s.url() }

// CSRFToken is the token the UI must echo back on every state-changing
// request. Production delivers it through the same-origin bootstrap API;
// this accessor exists for black-box security tests.
func (s *Server) CSRFToken() string { return s.csrfTok }

// Run blocks until ctx is cancelled or the idle timer elapses. The
// server reuses s.srv.Shutdown for graceful termination; the caller
// is expected to call Shutdown to release the port.
func (s *Server) Run(ctx context.Context) error {
	if s.ln == nil {
		return errors.New("webadmin: Run called before Listen")
	}
	idle := time.NewTimer(s.cfg.IdleTimeout)
	defer idle.Stop()
	stopped := make(chan error, 1)
	go func() {
		stopped <- s.srv.Serve(s.ln)
	}()
	if s.cfg.OpenBrowser != nil {
		go func() {
			// Slight delay so the server is fully bound by the
			// time the browser hits it.
			time.Sleep(50 * time.Millisecond)
			s.cfg.OpenBrowser(s.url())
		}()
	}
	for {
		select {
		case err := <-stopped:
			return err
		case <-ctx.Done():
			return s.Shutdown(context.Background())
		case <-s.activity:
			if !idle.Stop() {
				select {
				case <-idle.C:
				default:
				}
			}
			idle.Reset(s.cfg.IdleTimeout)
		case <-idle.C:
			s.cfg.Logger.Info("webadmin idle timeout; closing listener")
			return s.Shutdown(context.Background())
		}
	}
}

// Shutdown stops the HTTP server. It is safe to call multiple times.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.srv == nil {
		return nil
	}
	return s.srv.Shutdown(ctx)
}

// withSecurity wraps the mux in the middleware stack: Host allowlist,
// Origin check, CSRF for state-changing methods, CSP / nosniff /
// no-referrer headers and a request-size cap.
func (s *Server) withSecurity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		w.Header().Set("Cache-Control", "no-store")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store, max-age=0")
		}
		r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
		select {
		case s.activity <- struct{}{}:
		default:
		}
		// Host allowlist: only the bound loopback address is
		// accepted. Anything else returns 421 so DNS rebinding
		// cannot return useful data.
		if !s.hostAllowed(r.Host) {
			http.Error(w, "host not allowed", http.StatusMisdirectedRequest)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") && r.URL.RawQuery != "" {
			http.Error(w, "API query parameters are not allowed", http.StatusBadRequest)
			return
		}
		// Origin check: state-changing requests must come from the
		// same origin as the bound listener. Browsers always send
		// Origin for POST/PUT/DELETE.
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if !s.originAllowed(r.Header.Get("Origin")) {
				http.Error(w, "origin not allowed", http.StatusForbidden)
				return
			}
			if r.Header.Get("X-CSRF-Token") == "" ||
				subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")),
					[]byte(s.csrfTok)) != 1 {
				http.Error(w, "csrf token missing or invalid", http.StatusForbidden)
				return
			}
			if !s.allowMutation() {
				w.Header().Set("Retry-After", "60")
				http.Error(w, "request rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "application/json") {
				raw, err := io.ReadAll(r.Body)
				if err != nil {
					http.Error(w, "request body is too large", http.StatusRequestEntityTooLarge)
					return
				}
				if jsonDepth(raw) > 16 {
					http.Error(w, "JSON nesting is too deep", http.StatusBadRequest)
					return
				}
				r.Body = io.NopCloser(bytes.NewReader(raw))
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) allowMutation() bool {
	s.rateMu.Lock()
	defer s.rateMu.Unlock()
	now := s.cfg.Now()
	if s.rateStart.IsZero() || now.Sub(s.rateStart) >= time.Minute || now.Before(s.rateStart) {
		s.rateStart = now
		s.rateCount = 0
	}
	if s.rateCount >= s.cfg.MaxMutationsPerMinute {
		return false
	}
	s.rateCount++
	return true
}

func jsonDepth(raw []byte) int {
	depth, maxDepth := 0, 0
	inString, escaped := false, false
	for _, b := range raw {
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if b == '\\' {
				escaped = true
				continue
			}
			if b == '"' {
				inString = false
			}
			continue
		}
		switch b {
		case '"':
			inString = true
		case '{', '[':
			depth++
			if depth > maxDepth {
				maxDepth = depth
			}
		case '}', ']':
			if depth > 0 {
				depth--
			}
		}
	}
	return maxDepth
}

// hostAllowed returns true when r.Host is the listener's bound host
// (with or without an explicit port). DNS rebinding attempts that
// pass a different Host are rejected with 421.
func (s *Server) hostAllowed(reqHost string) bool {
	if s.addr == nil {
		return false
	}
	return reqHost == s.addr.String()
}

// originAllowed returns true when the Origin header (if any) refers
// to the same loopback listener. Browsers always include Origin for
// POST, so the absence of Origin is also rejected for state-changing
// methods.
func (s *Server) originAllowed(origin string) bool {
	if origin == "" {
		return false
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if u.Scheme != "http" {
		return false
	}
	if u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	return s.addr != nil && u.Host == s.addr.String()
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func staticHandler(sub fs.FS) http.Handler {
	fileSrv := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Reject any path with ".." so the embedded FS cannot be
		// tricked into serving a parent directory.
		if strings.Contains(r.URL.Path, "..") {
			http.NotFound(w, r)
			return
		}
		fileSrv.ServeHTTP(w, r)
	})
}

func randomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// portFromAddr is a tiny helper exposed for tests; it returns the
// numeric port of the bound listener or 0 when not bound yet.
func portFromAddr(addr net.Addr) int {
	if addr == nil {
		return 0
	}
	_, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return 0
	}
	p, _ := strconv.Atoi(port)
	return p
}
