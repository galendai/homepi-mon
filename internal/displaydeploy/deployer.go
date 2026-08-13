// Package displaydeploy implements the fixed-operation SSH transaction used
// by the local Web Admin to configure one HomePi display.
package displaydeploy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/displayconfig"
	"github.com/galendai/homepi-mon/internal/secretstore"
	"github.com/galendai/homepi-mon/internal/tlsconfig"
	"github.com/galendai/homepi-mon/internal/ui"
)

const SchemaVersion = 1

var safeHost = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,252}$`)

type Profile struct {
	SchemaVersion    int    `json:"schema_version"`
	SSHHost          string `json:"ssh_host"`
	DeviceID         string `json:"device_id"`
	NodeURL          string `json:"node_url"`
	Style            string `json:"style"`
	DataDir          string `json:"data_dir"`
	DeviceTokenRef   string `json:"device_token_ref"`
	PageOrder        string `json:"page_order,omitempty"`
	PageDwellSeconds string `json:"page_dwell_seconds,omitempty"`
}

type Edit struct {
	SSHHost          string `json:"ssh_host"`
	DeviceID         string `json:"device_id"`
	NodeURL          string `json:"node_url"`
	Style            string `json:"style"`
	DataDir          string `json:"data_dir"`
	PageOrder        string `json:"page_order"`
	PageDwellSeconds string `json:"page_dwell_seconds"`
}

type RemoteStatus struct {
	Connected        bool   `json:"connected"`
	Style            string `json:"style,omitempty"`
	DeviceID         string `json:"device_id,omitempty"`
	NodeURLHash      string `json:"node_url_hash,omitempty"`
	ServiceActive    bool   `json:"service_active"`
	Restarts         int    `json:"restarts"`
	SnapshotEpoch    string `json:"snapshot_epoch,omitempty"`
	SnapshotVersion  uint64 `json:"snapshot_version,omitempty"`
	SnapshotTime     string `json:"snapshot_time,omitempty"`
	BinaryVersion    string `json:"binary_version,omitempty"`
	Note             string `json:"note,omitempty"`
	PageOrder        string `json:"page_order,omitempty"`
	PageDwellSeconds string `json:"page_dwell_seconds,omitempty"`
}

type Result struct {
	Status     string       `json:"status"`
	RolledBack bool         `json:"rolled_back"`
	Error      string       `json:"error,omitempty"`
	Steps      []string     `json:"steps"`
	Remote     RemoteStatus `json:"remote"`
}

type State struct {
	Profile      Edit         `json:"profile"`
	Configured   bool         `json:"configured"`
	TokenPresent bool         `json:"token_present"`
	Tested       bool         `json:"tested"`
	Differences  []string     `json:"differences"`
	Remote       RemoteStatus `json:"remote"`
}

type Runner interface {
	Run(context.Context, string, string, []byte) ([]byte, error)
}

type SSHRunner struct{}

func (SSHRunner) Run(ctx context.Context, host, script string, stdin []byte) ([]byte, error) {
	if !safeHost.MatchString(host) {
		return nil, errors.New("display deploy: invalid SSH host")
	}
	cmd := exec.CommandContext(ctx, "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", host, script)
	cmd.Stdin = bytes.NewReader(stdin)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("display deploy: SSH operation failed: %s", sanitizeRemoteError(stderr.String()))
	}
	return out, nil
}

type Manager struct {
	configPath string
	dataDir    string
	store      secretstore.Store
	runner     Runner
	now        func() time.Time
	candidate  *Profile
	testedHash string
}

func New(configPath, dataDir string, store secretstore.Store, runner Runner) (*Manager, error) {
	if configPath == "" || dataDir == "" || store == nil {
		return nil, errors.New("display deploy: config path, data dir and secret store are required")
	}
	if runner == nil {
		runner = SSHRunner{}
	}
	return &Manager{configPath: configPath, dataDir: dataDir, store: store, runner: runner, now: time.Now}, nil
}

func (m *Manager) State(ctx context.Context) (State, error) {
	p, configured, err := m.currentProfile()
	if err != nil {
		return State{}, err
	}
	remote, probeErr := m.probe(ctx, p.SSHHost)
	state := State{Profile: publicEdit(p), Configured: configured, TokenPresent: m.tokenPresent(ctx, p.DeviceTokenRef), Tested: m.testedHash == profileHash(p), Remote: remote}
	if probeErr != nil {
		state.Remote.Note = probeErr.Error()
	}
	if m.candidate != nil {
		if _, statErr := os.Stat(m.profilePath()); statErr == nil {
			configured = true
			state.Configured = true
		}
	}
	if remote.Style != p.Style {
		state.Differences = append(state.Differences, "style")
	}
	if remote.DeviceID != "" && remote.DeviceID != p.DeviceID {
		state.Differences = append(state.Differences, "device_id")
	}
	wantURLHash := fmt.Sprintf("%x", sha256.Sum256([]byte(p.NodeURL)))
	if remote.NodeURLHash != "" && remote.NodeURLHash != wantURLHash {
		state.Differences = append(state.Differences, "node_url")
	}
	if remote.PageOrder != p.PageOrder {
		state.Differences = append(state.Differences, "page_order")
	}
	if remote.PageDwellSeconds != p.PageDwellSeconds {
		state.Differences = append(state.Differences, "page_dwell_seconds")
	}
	return state, nil
}

// Target returns the configured display device without probing SSH. Web
// Admin command publication uses this narrow read so a temporary deployment
// outage does not block the already authenticated local control channel.
func (m *Manager) Target(context.Context) (string, error) {
	p, _, err := m.currentProfile()
	if err != nil {
		return "", err
	}
	return p.DeviceID, nil
}

func (m *Manager) Edit(ctx context.Context, edit Edit) (State, error) {
	p, err := m.derive(ctx, edit)
	if err != nil {
		return State{}, err
	}
	m.candidate = &p
	m.testedHash = ""
	return m.State(ctx)

}

func (m *Manager) Test(ctx context.Context) (Result, error) {
	p, _, err := m.currentProfile()
	if err != nil {
		return Result{}, err
	}
	env, err := m.environment(ctx, p)
	if err != nil {
		return Result{}, err
	}
	raw, err := env.Render()
	if err != nil {
		return Result{}, err
	}
	if _, err := m.runner.Run(ctx, p.SSHHost, validateScript, raw); err != nil {
		return Result{}, err
	}
	remote, err := m.probe(ctx, p.SSHHost)
	if err != nil {
		return Result{}, err
	}
	m.testedHash = profileHash(p)
	return Result{Status: "tested", Steps: []string{"profile_validated", "ssh_connected", "candidate_validated"}, Remote: remote}, nil
}

func (m *Manager) Apply(ctx context.Context) (Result, error) {
	p, _, err := m.currentProfile()
	if err != nil {
		return Result{}, err
	}
	if m.testedHash == "" || m.testedHash != profileHash(p) {
		return Result{}, errors.New("display deploy: candidate must pass test before apply")
	}
	env, err := m.environment(ctx, p)
	if err != nil {
		return Result{}, err
	}
	raw, err := env.Render()
	if err != nil {
		return Result{}, err
	}
	before, err := m.probe(ctx, p.SSHHost)
	if err != nil {
		return Result{}, err
	}
	out, err := m.runner.Run(ctx, p.SSHHost, applyScript, raw)
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(string(out)) != "applied" {
		return Result{}, errors.New("display deploy: unexpected apply response")
	}
	remote, err := m.waitHealthy(ctx, p.SSHHost, before)
	if err != nil {
		_, _ = m.runner.Run(context.Background(), p.SSHHost, rollbackScript, nil)
		m.testedHash = ""
		wrapped := fmt.Errorf("display deploy: health confirmation failed: %w", err)
		return Result{Status: "rolled_back", RolledBack: true, Error: wrapped.Error(), Steps: []string{"candidate_installed", "service_restarted", "health_failed", "environment_rolled_back"}}, wrapped
	}
	if err := m.saveProfile(p); err != nil {
		_, _ = m.runner.Run(context.Background(), p.SSHHost, rollbackScript, nil)
		m.testedHash = ""
		wrapped := fmt.Errorf("display deploy: save profile: %w", err)
		return Result{Status: "rolled_back", RolledBack: true, Error: wrapped.Error()}, wrapped
	}
	m.candidate = nil
	m.testedHash = ""
	return Result{Status: "applied", Steps: []string{"candidate_installed", "service_restarted", "service_active", "snapshot_confirmed", "profile_saved"}, Remote: remote}, nil
}

func (m *Manager) currentProfile() (Profile, bool, error) {
	if m.candidate != nil {
		return *m.candidate, false, nil
	}
	raw, err := os.ReadFile(m.profilePath())
	if err == nil {
		var p Profile
		if json.Unmarshal(raw, &p) != nil || p.SchemaVersion != SchemaVersion {
			return Profile{}, false, errors.New("display deploy: invalid display-profiles.json")
		}
		return normalizeProfile(p), true, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Profile{}, false, fmt.Errorf("display deploy: read profile: %w", err)
	}
	cfg, err := config.Load(m.configPath)
	if err != nil {
		return Profile{}, false, err
	}
	if len(cfg.Devices) == 0 {
		return Profile{}, false, errors.New("display deploy: no paired device")
	}
	return normalizeProfile(Profile{SchemaVersion: SchemaVersion, SSHHost: "dietpi", DeviceID: cfg.Devices[0].ID, NodeURL: "https://" + cfg.Listen.Addr, Style: "rich", DataDir: displayconfig.DefaultDataDir, DeviceTokenRef: cfg.Devices[0].TokenRef}), false, nil
}

func (m *Manager) derive(ctx context.Context, edit Edit) (Profile, error) {
	if !safeHost.MatchString(edit.SSHHost) {
		return Profile{}, errors.New("display deploy: invalid SSH host")
	}
	cfg, err := config.Load(m.configPath)
	if err != nil {
		return Profile{}, err
	}
	var tokenRef string
	for _, d := range cfg.Devices {
		if d.ID == edit.DeviceID {
			tokenRef = d.TokenRef
			break
		}
	}
	if tokenRef == "" {
		return Profile{}, errors.New("display deploy: device is not paired")
	}
	p := normalizeProfile(Profile{SchemaVersion: SchemaVersion, SSHHost: edit.SSHHost, DeviceID: edit.DeviceID, NodeURL: edit.NodeURL, Style: edit.Style, DataDir: edit.DataDir, DeviceTokenRef: tokenRef, PageOrder: edit.PageOrder, PageDwellSeconds: edit.PageDwellSeconds})
	if _, err := m.environment(ctx, p); err != nil {
		return Profile{}, err
	}
	return p, nil
}

func (m *Manager) environment(ctx context.Context, p Profile) (displayconfig.Environment, error) {
	cfg, err := config.Load(m.configPath)
	if err != nil {
		return displayconfig.Environment{}, err
	}
	token, err := m.store.Get(ctx, p.DeviceTokenRef)
	if err != nil {
		return displayconfig.Environment{}, errors.New("display deploy: device token is unavailable")
	}
	certPath := cfg.Listen.TLS.Cert
	if certPath == "" || certPath == "auto" {
		certPath = filepath.Join(m.dataDir, "cert.pem")
	}
	pin, err := tlsconfig.Fingerprint(certPath)
	if err != nil {
		return displayconfig.Environment{}, err
	}
	env := displayconfig.Environment{NodeURL: p.NodeURL, DeviceID: p.DeviceID, SourceNode: cfg.SourceNode.ID, CertPin: pin, DeviceToken: token, DataDir: p.DataDir, Style: p.Style, PageOrder: p.PageOrder, PageDwellSeconds: p.PageDwellSeconds}
	return env, env.Validate()
}

func (m *Manager) tokenPresent(ctx context.Context, ref string) bool {
	if ref == "" {
		return false
	}
	_, err := m.store.Get(ctx, ref)
	return err == nil
}

func (m *Manager) probe(ctx context.Context, host string) (RemoteStatus, error) {
	if !safeHost.MatchString(host) {
		return RemoteStatus{}, errors.New("display deploy: invalid SSH host")
	}
	out, err := m.runner.Run(ctx, host, probeScript, nil)
	if err != nil {
		return RemoteStatus{}, err
	}
	return parseProbe(out)
}

func (m *Manager) waitHealthy(ctx context.Context, host string, before RemoteStatus) (RemoteStatus, error) {
	deadline := m.now().Add(25 * time.Second)
	for {
		status, err := m.probe(ctx, host)
		advanced := status.SnapshotEpoch != "" && (status.SnapshotEpoch != before.SnapshotEpoch || status.SnapshotVersion > before.SnapshotVersion)
		if err == nil && status.ServiceActive && status.Restarts == 0 && advanced {
			return status, nil
		}
		if !m.now().Before(deadline) {
			if err != nil {
				return RemoteStatus{}, err
			}
			return RemoteStatus{}, errors.New("display service did not become healthy")
		}
		select {
		case <-ctx.Done():
			return RemoteStatus{}, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func parseProbe(raw []byte) (RemoteStatus, error) {
	var s RemoteStatus
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch key {
		case "active":
			s.ServiceActive = value == "active"
		case "style":
			s.Style = value
		case "device_id":
			s.DeviceID = value
		case "node_url_hash":
			s.NodeURLHash = value
		case "restarts":
			s.Restarts, _ = strconv.Atoi(value)
		case "epoch":
			s.SnapshotEpoch = value
		case "version":
			s.SnapshotVersion, _ = strconv.ParseUint(value, 10, 64)
		case "generated":
			s.SnapshotTime = value
		case "binary":
			s.BinaryVersion = value
		case "page_order":
			s.PageOrder = value
		case "page_dwell_seconds":
			s.PageDwellSeconds = value
		}
	}
	s.Connected = s.ServiceActive && s.SnapshotVersion > 0 && s.SnapshotEpoch != ""
	if s.BinaryVersion == "" {
		return s, errors.New("display deploy: incomplete remote status")
	}
	return s, nil
}

func (m *Manager) saveProfile(p Profile) error {
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(m.dataDir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(m.dataDir, "display-profiles.json.tmp-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, m.profilePath())
}

func (m *Manager) profilePath() string { return filepath.Join(m.dataDir, "display-profiles.json") }

func publicEdit(p Profile) Edit {
	return Edit{SSHHost: p.SSHHost, DeviceID: p.DeviceID, NodeURL: p.NodeURL, Style: p.Style, DataDir: p.DataDir, PageOrder: p.PageOrder, PageDwellSeconds: p.PageDwellSeconds}
}

func normalizeProfile(p Profile) Profile {
	if p.PageOrder == "" {
		p.PageOrder = ui.DefaultPageOrderText
	}
	if p.PageDwellSeconds == "" {
		p.PageDwellSeconds = ui.DefaultPageDwellText
	}
	return p
}

func profileHash(p Profile) string {
	raw, _ := json.Marshal(p)
	return string(raw)
}

func sanitizeRemoteError(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "no diagnostic output"
	}
	if len(s) > 240 {
		s = s[:240]
	}
	return strings.Map(func(r rune) rune {
		if r < 0x20 {
			return ' '
		}
		return r
	}, s)
}

const probeScript = `set -eu
test ! -L /etc/homepi-display/environment
test -f /etc/homepi-display/environment
test "$(stat -c %U:%G:%a /etc/homepi-display/environment)" = root:root:600
printf 'active=%s\n' "$(systemctl is-active homepi-display.service 2>/dev/null || true)"
printf 'restarts=%s\n' "$(systemctl show homepi-display.service -p NRestarts --value)"
printf 'style=%s\n' "$(sed -n 's/^HOMEPI_DISPLAY_STYLE=//p' /etc/homepi-display/environment | head -n1)"
printf 'device_id=%s\n' "$(sed -n 's/^HOMEPI_DEVICE_ID=//p' /etc/homepi-display/environment | head -n1)"
printf 'page_order=%s\n' "$(sed -n 's/^HOMEPI_PAGE_ORDER=//p' /etc/homepi-display/environment | head -n1)"
printf 'page_dwell_seconds=%s\n' "$(sed -n 's/^HOMEPI_PAGE_DWELL_SECONDS=//p' /etc/homepi-display/environment | head -n1)"
node_url="$(sed -n 's/^HOMEPI_NODE_URL=//p' /etc/homepi-display/environment | head -n1)"
printf 'node_url_hash=%s\n' "$(printf %s "$node_url" | sha256sum | cut -d' ' -f1)"
printf 'binary=%s\n' "$(homepi-display version | head -n1 | tr ' ' '_')"
snapshot=/var/lib/homepi-display/last-known-good.json
if [ -f "$snapshot" ] && [ ! -L "$snapshot" ]; then
  printf 'epoch=%s\n' "$(sed -n 's/.*"source_epoch":"\([^"]*\)".*/\1/p' "$snapshot" | head -n1)"
  printf 'version=%s\n' "$(sed -n 's/.*"snapshot_version":\([0-9][0-9]*\).*/\1/p' "$snapshot" | head -n1)"
  printf 'generated=%s\n' "$(sed -n 's/.*"generated_at":"\([^"]*\)".*/\1/p' "$snapshot" | head -n1)"
else
  printf 'epoch=\nversion=0\ngenerated=\n'
fi`

const validateScript = `set -eu
candidate=/etc/homepi-display/.environment.candidate
trap 'rm -f "$candidate"' EXIT HUP INT TERM
test ! -e "$candidate"
umask 077
cat > "$candidate"
chown root:root "$candidate"
chmod 600 "$candidate"
homepi-display config validate --file "$candidate" >/dev/null
printf 'validated\n'`

const applyScript = `set -eu
env=/etc/homepi-display/environment
candidate=/etc/homepi-display/.environment.candidate
previous=/etc/homepi-display/environment.previous
trap 'rm -f "$candidate"' EXIT HUP INT TERM
test ! -L "$env"
test -f "$env"
test "$(stat -c %U:%G:%a "$env")" = root:root:600
test ! -L "$previous"
umask 077
cat > "$candidate"
chown root:root "$candidate"
chmod 600 "$candidate"
homepi-display config validate --file "$candidate" >/dev/null
rm -f "$previous"
mv "$env" "$previous"
mv "$candidate" "$env"
if ! systemctl restart homepi-display.service || ! systemctl is-active --quiet homepi-display.service; then
  mv "$previous" "$env"
  systemctl restart homepi-display.service || true
  echo rolled_back >&2
  exit 41
fi
sleep 2
if [ "$(systemctl show homepi-display.service -p NRestarts --value)" != 0 ]; then
  mv "$previous" "$env"
  systemctl restart homepi-display.service || true
  echo rolled_back >&2
  exit 42
fi
printf 'applied\n'`

const rollbackScript = `set -eu
env=/etc/homepi-display/environment
previous=/etc/homepi-display/environment.previous
test ! -L "$previous"
test -f "$previous"
cp "$previous" /etc/homepi-display/.environment.rollback
chown root:root /etc/homepi-display/.environment.rollback
chmod 600 /etc/homepi-display/.environment.rollback
homepi-display config validate --file /etc/homepi-display/.environment.rollback >/dev/null
mv /etc/homepi-display/.environment.rollback "$env"
systemctl restart homepi-display.service
systemctl is-active --quiet homepi-display.service
printf 'rolled_back\n'`
