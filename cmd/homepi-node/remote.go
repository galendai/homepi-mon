package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/secretstore"
	"github.com/galendai/homepi-mon/internal/tlsconfig"
)

type remoteFlags struct {
	configPath string
	deviceID   string
	nodeURL    string
	wait       bool
	waitFor    time.Duration
	noTLS      bool
}

type remoteResponse struct {
	CommandID string                 `json:"command_id"`
	DeviceID  string                 `json:"device_id"`
	Kind      protocol.CommandKind   `json:"kind"`
	Sequence  uint64                 `json:"sequence"`
	IssuedAt  string                 `json:"issued_at"`
	ExpiresAt string                 `json:"expires_at"`
	Result    protocol.CommandResult `json:"result"`
}

func runRemote(args []string) error {
	f := remoteFlags{}
	fs := flag.NewFlagSet("remote", flag.ContinueOnError)
	fs.StringVar(&f.configPath, "config", config.DefaultPath(), "path to config.json")
	fs.StringVar(&f.deviceID, "device", "", "target display device ID; optional when exactly one is configured")
	fs.StringVar(&f.nodeURL, "node-url", "", "override the local daemon URL")
	fs.BoolVar(&f.wait, "wait", true, "wait for a final display result")
	fs.DurationVar(&f.waitFor, "wait-timeout", 35*time.Second, "maximum wait for a final result")
	fs.BoolVar(&f.noTLS, "no-tls", false, "use loopback HTTP for an explicitly insecure development daemon")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: homepi-node remote [flags] ACTION [action flags] [arguments]")
		fmt.Fprintln(fs.Output(), "Actions: show-page, next-page, previous-page, set-rotation, refresh, show-message, set-brightness")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() == 0 {
		return errors.New("remote: an action is required")
	}
	if f.waitFor <= 0 {
		return errors.New("remote: wait-timeout must be positive")
	}
	request, err := parseRemoteAction(fs.Arg(0), fs.Args()[1:])
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	cfg, err := config.Load(f.configPath)
	if err != nil {
		return err
	}
	deviceID, err := selectRemoteDevice(cfg, f.deviceID)
	if err != nil {
		return err
	}
	dataDir, err := defaultDataDir()
	if err != nil {
		return err
	}
	store, err := secretstore.OpenFromEnv(context.Background())
	if err != nil {
		return err
	}
	controlToken, err := store.Get(context.Background(), controlTokenRef)
	if err != nil {
		return fmt.Errorf("remote control is not initialized; start the current homepi-node service first: %w", err)
	}
	baseURL := f.nodeURL
	if baseURL == "" {
		baseURL, err = localControlURL(cfg.Listen.Addr, f.noTLS)
		if err != nil {
			return err
		}
	}
	client, err := remoteHTTPClient(cfg, dataDir, baseURL, f.noTLS)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), f.waitFor)
	defer cancel()
	response, err := publishRemote(ctx, client, baseURL, controlToken, deviceID, request)
	if err != nil {
		return err
	}
	if f.wait {
		response, err = waitRemote(ctx, client, baseURL, controlToken, response.CommandID)
		if err != nil {
			return err
		}
	}
	body, _ := json.MarshalIndent(response, "", "  ")
	fmt.Println(string(body))
	if response.Result.Status == protocol.CommandRejected || response.Result.Status == protocol.CommandExpired ||
		response.Result.Status == protocol.CommandFailed {
		return fmt.Errorf("remote command %s: %s", response.Result.Status, response.Result.Code)
	}
	return nil
}

func parseRemoteAction(action string, args []string) (protocol.CommandRequest, error) {
	request := protocol.CommandRequest{Priority: protocol.PriorityNormal, TTLSeconds: int(protocol.DefaultCommandTTL / time.Second)}
	params := any(nil)
	fs := flag.NewFlagSet("remote "+action, flag.ContinueOnError)
	duration := fs.Duration("duration", protocol.DefaultPageDuration, "temporary display duration")
	ttl := fs.Duration("ttl", protocol.DefaultCommandTTL, "delivery time-to-live")
	interval := fs.Duration("interval", 0, "temporary rotation interval")
	severity := fs.String("severity", "info", "message severity: info, warning or critical")
	if err := fs.Parse(args); err != nil {
		return request, err
	}
	durationSet := false
	allowedFlags := map[string]bool{"ttl": true}
	switch action {
	case "show-page", "next-page", "previous-page", "show-message", "set-rotation":
		allowedFlags["duration"] = true
	}
	if action == "show-message" {
		allowedFlags["severity"] = true
	}
	if action == "set-rotation" {
		allowedFlags["interval"] = true
	}
	var unsupportedFlag string
	fs.Visit(func(item *flag.Flag) {
		if item.Name == "duration" {
			durationSet = true
		}
		if !allowedFlags[item.Name] {
			unsupportedFlag = item.Name
		}
	})
	if unsupportedFlag != "" {
		return request, fmt.Errorf("remote %s does not support --%s", action, unsupportedFlag)
	}
	if action == "set-rotation" && !durationSet {
		*duration = 5 * time.Minute
	}
	durationSeconds, err := remoteDurationSeconds(*duration)
	if err != nil {
		return request, err
	}
	ttlSeconds, err := remoteDurationSeconds(*ttl)
	if err != nil {
		return request, err
	}
	intervalSeconds, err := remoteDurationSeconds(*interval)
	if err != nil {
		return request, err
	}
	request.TTLSeconds = ttlSeconds
	switch action {
	case "show-page":
		if fs.NArg() != 1 {
			return request, errors.New("remote show-page requires one page ID")
		}
		request.Kind = protocol.CommandShowPage
		params = protocol.PageCommandParams{PageID: strings.ToUpper(fs.Arg(0)), DurationSeconds: durationSeconds}
	case "next-page":
		if fs.NArg() != 0 {
			return request, errors.New("remote next-page takes no positional arguments")
		}
		request.Kind = protocol.CommandNextPage
		params = protocol.StepPageCommandParams{DurationSeconds: durationSeconds}
	case "previous-page":
		if fs.NArg() != 0 {
			return request, errors.New("remote previous-page takes no positional arguments")
		}
		request.Kind = protocol.CommandPreviousPage
		params = protocol.StepPageCommandParams{DurationSeconds: durationSeconds}
	case "set-rotation":
		if fs.NArg() != 1 || (fs.Arg(0) != "on" && fs.Arg(0) != "off") {
			return request, errors.New("remote set-rotation requires on or off")
		}
		request.Kind = protocol.CommandSetRotation
		params = protocol.RotationCommandParams{
			Enabled: fs.Arg(0) == "on", IntervalSeconds: intervalSeconds,
			DurationSeconds: durationSeconds,
		}
	case "refresh":
		request.Kind = protocol.CommandRefreshData
		params = protocol.RefreshCommandParams{ConnectorIDs: fs.Args()}
	case "show-message":
		if fs.NArg() == 0 {
			return request, errors.New("remote show-message requires text")
		}
		request.Kind, request.Priority = protocol.CommandShowMessage, protocol.PriorityHigh
		params = protocol.MessageCommandParams{
			Text: strings.Join(fs.Args(), " "), Severity: *severity, DurationSeconds: durationSeconds,
		}
	case "set-brightness":
		if fs.NArg() != 1 {
			return request, errors.New("remote set-brightness requires a level")
		}
		level, err := strconv.Atoi(fs.Arg(0))
		if err != nil {
			return request, errors.New("remote set-brightness level must be an integer")
		}
		request.Kind = protocol.CommandSetBrightness
		params = protocol.BrightnessCommandParams{Level: level}
	default:
		return request, fmt.Errorf("remote: unknown action %q", action)
	}
	request.Params, _ = json.Marshal(params)
	if err := protocol.ValidateCommandRequest(request); err != nil {
		return request, err
	}
	return request, nil
}

func remoteDurationSeconds(value time.Duration) (int, error) {
	if value < 0 || value > protocol.MaxCommandDuration || value%time.Second != 0 {
		return 0, errors.New("remote durations must use whole seconds in the range 0s..5m")
	}
	return int(value / time.Second), nil
}

func selectRemoteDevice(cfg *config.Config, requested string) (string, error) {
	if requested != "" {
		for _, device := range cfg.Devices {
			if device.ID == requested {
				return requested, nil
			}
		}
		return "", fmt.Errorf("remote target device %q is not configured", requested)
	}
	if len(cfg.Devices) != 1 {
		return "", errors.New("remote --device is required unless exactly one display is configured")
	}
	return cfg.Devices[0].ID, nil
}

func localControlURL(addr string, noTLS bool) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("remote listen address: %w", err)
	}
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::", "[::]":
		host = "::1"
	}
	if noTLS && net.ParseIP(host) != nil && !net.ParseIP(host).IsLoopback() {
		return "", errors.New("remote --no-tls is restricted to loopback")
	}
	scheme := "https"
	if noTLS {
		scheme = "http"
	}
	return scheme + "://" + net.JoinHostPort(host, port), nil
}

func remoteHTTPClient(cfg *config.Config, dataDir, baseURL string, noTLS bool) (*http.Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" {
		return nil, errors.New("remote node URL is invalid")
	}
	if noTLS {
		if u.Scheme != "http" || !isLoopbackName(u.Hostname()) {
			return nil, errors.New("remote insecure HTTP is restricted to loopback")
		}
		return &http.Client{Timeout: 10 * time.Second}, nil
	}
	if u.Scheme != "https" {
		return nil, errors.New("remote control requires HTTPS")
	}
	certPath := cfg.Listen.TLS.Cert
	if certPath == "auto" {
		certPath = filepath.Join(dataDir, "cert.pem")
	}
	fingerprint, err := tlsconfig.Fingerprint(certPath)
	if err != nil {
		return nil, err
	}
	pin, err := tlsconfig.ParseFingerprint(fingerprint)
	if err != nil {
		return nil, err
	}
	transport := tlsconfig.PinningTransport(pin)
	transport.DialContext = (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	transport.TLSHandshakeTimeout = 5 * time.Second
	transport.ResponseHeaderTimeout = 5 * time.Second
	return &http.Client{Transport: transport, Timeout: 10 * time.Second}, nil
}

func isLoopbackName(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func publishRemote(ctx context.Context, client *http.Client, baseURL, token, deviceID string,
	request protocol.CommandRequest) (remoteResponse, error) {
	body, _ := json.Marshal(request)
	endpoint := strings.TrimSuffix(baseURL, "/") + "/v1/control/devices/" + url.PathEscape(deviceID) + "/commands"
	return doRemote(ctx, client, http.MethodPost, endpoint, token, body)
}

func waitRemote(ctx context.Context, client *http.Client, baseURL, token, commandID string) (remoteResponse, error) {
	endpoint := strings.TrimSuffix(baseURL, "/") + "/v1/control/commands/" + url.PathEscape(commandID)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		response, err := doRemote(ctx, client, http.MethodGet, endpoint, token, nil)
		if err != nil {
			return remoteResponse{}, err
		}
		if response.Result.Status.Final() {
			return response, nil
		}
		select {
		case <-ctx.Done():
			return remoteResponse{}, fmt.Errorf("wait for remote command: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func doRemote(ctx context.Context, client *http.Client, method, endpoint, token string, body []byte) (remoteResponse, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return remoteResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return remoteResponse{}, fmt.Errorf("remote control request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, protocol.MaxCommandBytes))
	if err != nil {
		return remoteResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var streamErr protocol.StreamError
		_ = json.Unmarshal(raw, &streamErr)
		if streamErr.Code == "" {
			streamErr.Code = resp.Status
		}
		return remoteResponse{}, fmt.Errorf("remote control rejected: %s", streamErr.Code)
	}
	var result remoteResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		return remoteResponse{}, errors.New("remote control returned an invalid response")
	}
	return result, nil
}
