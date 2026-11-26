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
	"strings"
	"sync"
	"time"

	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/displayconfig"
	"github.com/galendai/homepi-mon/internal/secretstore"
	"github.com/galendai/homepi-mon/internal/tlsconfig"
)

const SchemaVersion = 1

var safeHost = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,252}$`)

var ErrEnvironmentChanged = errors.New("display deploy: the Pi environment changed since the SSH test; review the file and test again")

type Profile struct {
	Edit
	SchemaVersion  int    `json:"schema_version"`
	DeviceTokenRef string `json:"device_token_ref"`
}

type Edit struct {
	SSHHost          string `json:"ssh_host"`
	DeviceID         string `json:"device_id"`
	NodeURL          string `json:"node_url"`
	Style            string `json:"style"`
	DataDir          string `json:"data_dir"`
	PageOrder        string `json:"page_order"`
	PageDwellSeconds string `json:"page_dwell_seconds"`
	HomeOnly         string `json:"home_only"`
	PageSubset       string `json:"page_subset,omitempty"`
	PageLock         string `json:"page_lock,omitempty"`
}

type RemoteStatus struct {
	displayconfig.PageSettings
	fileDigest      string
	Verified        bool           `json:"verified"`
	SupportsPageEnv bool           `json:"supports_page_env"`
	DropIns         []string       `json:"drop_ins,omitempty"`
	StartupState    string         `json:"startup_state,omitempty"`
	Startup         *StartupStatus `json:"startup,omitempty"`
	Connected       bool           `json:"connected"`
	DeviceID        string         `json:"device_id,omitempty"`
	NodeURLHash     string         `json:"node_url_hash,omitempty"`
	ServiceActive   bool           `json:"service_active"`
	Restarts        int            `json:"restarts"`
	SnapshotEpoch   string         `json:"snapshot_epoch,omitempty"`
	SnapshotVersion uint64         `json:"snapshot_version,omitempty"`
	SnapshotTime    string         `json:"snapshot_time,omitempty"`
	BinaryVersion   string         `json:"binary_version,omitempty"`
	Note            string         `json:"note,omitempty"`
}

type Result struct {
	Revision   string       `json:"revision,omitempty"`
	Status     string       `json:"status"`
	RolledBack bool         `json:"rolled_back"`
	Error      string       `json:"error,omitempty"`
	Steps      []string     `json:"steps"`
	Remote     RemoteStatus `json:"remote"`
}

type State struct {
	Revision       string       `json:"revision"`
	TargetDeviceID string       `json:"target_device_id,omitempty"`
	Profile        Edit         `json:"profile"`
	Configured     bool         `json:"configured"`
	TokenPresent   bool         `json:"token_present"`
	Tested         bool         `json:"tested"`
	Differences    []string     `json:"differences"`
	Remote         RemoteStatus `json:"remote"`
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
	mu            sync.Mutex
	configPath    string
	dataDir       string
	store         secretstore.Store
	runner        Runner
	now           func() time.Time
	candidate     *Profile
	testedHash    string
	testedFile    string
	testedPayload [sha256.Size]byte
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
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state(ctx)
}

func (m *Manager) state(ctx context.Context) (State, error) {
	p, saved, err := m.savedProfile()
	if err != nil {
		return State{}, err
	}
	var target string
	if saved {
		target = p.DeviceID
	}
	if m.candidate != nil {
		p = *m.candidate
	}
	remote, probeErr := m.probe(ctx, p.SSHHost)
	revision := profileHash(p)
	state := State{Profile: p.Edit, Revision: revision, TargetDeviceID: target, Configured: target != "",
		TokenPresent: m.tokenPresent(ctx, p.DeviceTokenRef), Tested: remote.Verified && m.testedHash == revision && m.testedFile == remote.fileDigest, Remote: remote}
	if probeErr != nil {
		state.Remote.Note = probeErr.Error()
		return state, nil
	}
	state.Differences = fileDifferences(p, remote)
	return state, nil
}

func fileDifferences(p Profile, remote RemoteStatus) []string {
	var differences []string
	if remote.Style != p.Style {
		differences = append(differences, "style")
	}
	if remote.DeviceID != p.DeviceID {
		differences = append(differences, "device_id")
	}
	wantURLHash := fmt.Sprintf("%x", sha256.Sum256([]byte(p.NodeURL)))
	if remote.NodeURLHash != wantURLHash {
		differences = append(differences, "node_url")
	}
	if remote.PageOrder != p.PageOrder {
		differences = append(differences, "page_order")
	}
	if remote.PageDwellSeconds != p.PageDwellSeconds {
		differences = append(differences, "page_dwell_seconds")
	}
	if remote.HomeOnly != p.HomeOnly {
		differences = append(differences, "home_only")
	}
	if remote.PageSubset != p.PageSubset {
		differences = append(differences, "page_subset")
	}
	if remote.PageLock != p.PageLock {
		differences = append(differences, "page_lock")
	}
	return differences
}

// Target returns the configured display device without probing SSH. Web
// Admin command publication uses this narrow read so a temporary deployment
// outage does not block the already authenticated local control channel.
func (m *Manager) Target(context.Context) (string, error) {
	p, saved, err := m.savedProfile()
	if err != nil || !saved {
		return "", err
	}
	return p.DeviceID, nil
}

func (m *Manager) Edit(ctx context.Context, edit Edit) (State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, err := m.derive(ctx, edit)
	if err != nil {
		return State{}, err
	}
	m.candidate = &p
	m.testedHash = ""
	return m.state(ctx)
}

// CandidateRevision lets the HTTP handlers bind a browser action to its last
// saved draft. Their transaction mutex covers the comparison and the action.
func (m *Manager) CandidateRevision() (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, _, err := m.currentProfile()
	if err != nil {
		return "", err
	}
	return profileHash(p), nil
}

func checkPageCapability(p Profile, remote RemoteStatus) error {
	if (p.PageSubset != "" || p.PageLock != "") && !remote.SupportsPageEnv {
		return errors.New("display deploy: update homepi-display on the Pi to use environment-based custom pages or page lock")
	}
	return nil
}

func (m *Manager) Test(ctx context.Context) (Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.testedHash = ""
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
	remote, err := m.probe(ctx, p.SSHHost)
	if err != nil {
		return Result{}, err
	}
	if err := checkPageCapability(p, remote); err != nil {
		return Result{}, err
	}
	out, err := m.runner.Run(ctx, p.SSHHost, validateScript, raw)
	if err != nil {
		return Result{}, err
	}
	if strings.TrimSpace(string(out)) != "validated" {
		return Result{}, errors.New("display deploy: candidate validation was not confirmed")
	}
	m.testedHash = profileHash(p)
	m.testedFile = remote.fileDigest
	m.testedPayload = sha256.Sum256(raw)
	return Result{Status: "tested", Revision: m.testedHash, Steps: []string{"profile_validated", "ssh_connected", "candidate_validated"}, Remote: remote}, nil
}

func (m *Manager) Apply(ctx context.Context) (Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, _, err := m.currentProfile()
	if err != nil {
		return Result{}, err
	}
	if m.testedHash == "" || m.testedHash != profileHash(p) {
		return Result{}, errors.New("display deploy: candidate must pass test before apply")
	}
	m.testedHash = ""
	env, err := m.environment(ctx, p)
	if err != nil {
		return Result{}, err
	}
	raw, err := env.Render()
	if err != nil {
		return Result{}, err
	}
	if sha256.Sum256(raw) != m.testedPayload {
		return Result{}, errors.New("display deploy: updated candidate credentials must pass test before apply")
	}
	before, err := m.probe(ctx, p.SSHHost)
	if err != nil {
		return Result{}, err
	}
	if err := checkPageCapability(p, before); err != nil {
		return Result{}, err
	}
	if before.fileDigest != m.testedFile {
		return Result{}, ErrEnvironmentChanged
	}
	// Both operations check the expected file under the remote directory lock.
	// Keep the digest and candidate on stdin and out of SSH arguments and JSON.
	transaction := append([]byte(before.fileDigest+"\n"), raw...)
	out, err := m.runner.Run(ctx, p.SSHHost, applyScript, transaction)
	if err != nil {
		return recoveryRequired(fmt.Errorf("display deploy: installation was not confirmed: %w", err))
	}
	switch strings.TrimSpace(string(out)) {
	case "applied":
	case "environment_changed":
		return Result{}, ErrEnvironmentChanged
	case "restart_failed":
		return m.rollback(p.SSHHost, before, transaction, errors.New("display deploy: service restart failed"))
	default:
		return recoveryRequired(errors.New("display deploy: unexpected installation response"))
	}
	remote, err := m.waitHealthy(ctx, p.SSHHost, before)
	if err != nil {
		return m.rollback(p.SSHHost, before, transaction, fmt.Errorf("display deploy: health confirmation failed: %w", err))
	}
	if diff := fileDifferences(p, remote); len(diff) != 0 {
		return m.rollback(p.SSHHost, before, transaction, fmt.Errorf("display deploy: installed fields differ: %s", strings.Join(diff, ", ")))
	}
	if remote.fileDigest != fmt.Sprintf("%x", m.testedPayload) {
		return m.rollback(p.SSHHost, before, transaction, errors.New("display deploy: installed environment differs from the tested candidate"))
	}
	if err := m.saveProfile(p); err != nil {
		return m.rollback(p.SSHHost, before, transaction, fmt.Errorf("display deploy: save profile: %w", err))
	}
	m.candidate = nil
	return Result{Status: "applied", Revision: profileHash(p), Steps: []string{"environment_confirmed", "service_active", "snapshot_confirmed", remote.StartupState, "profile_saved"}, Remote: remote}, nil
}

func (s RemoteStatus) startupStep() string {
	if s.Startup == nil {
		return "startup_unverified"
	}
	if len(s.Startup.Flags) != 0 {
		return "startup_overridden"
	}
	if s.Startup.PageSettings != s.PageSettings {
		return "startup_differs"
	}
	return "startup_confirmed"
}

func recoveryRequired(err error) (Result, error) {
	return Result{Status: "manual_recovery_required", Error: err.Error()}, err
}

func (m *Manager) rollback(host string, before RemoteStatus, transaction []byte, cause error) (Result, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	probeCtx, finishProbe := context.WithTimeout(ctx, 10*time.Second)
	baseline, probeErr := m.probe(probeCtx, host)
	finishProbe()
	out, err := m.runner.Run(ctx, host, rollbackScript, transaction)
	if err == nil && strings.TrimSpace(string(out)) != "rolled_back" {
		err = errors.New("unexpected rollback response")
	}
	if err == nil && probeErr != nil {
		err = fmt.Errorf("rollback snapshot baseline unavailable: %w", probeErr)
	}
	var restored RemoteStatus
	if err == nil {
		restored, err = m.waitHealthy(ctx, host, baseline)
	}
	if err == nil && restored.fileDigest != before.fileDigest {
		err = errors.New("restored environment differs from the previous file")
	}
	if err != nil {
		return recoveryRequired(fmt.Errorf("%w; could not confirm recovery: %v", cause, err))
	}
	return Result{Status: "rolled_back", RolledBack: true, Error: cause.Error(), Steps: []string{"previous_environment_verified", "service_active", "snapshot_confirmed"}, Remote: restored}, cause
}

func (m *Manager) currentProfile() (Profile, bool, error) {
	if m.candidate != nil {
		return *m.candidate, false, nil
	}
	return m.savedProfile()
}

func (m *Manager) savedProfile() (Profile, bool, error) {
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
	return normalizeProfile(Profile{SchemaVersion: SchemaVersion, DeviceTokenRef: cfg.Devices[0].TokenRef, Edit: Edit{
		SSHHost: "dietpi", DeviceID: cfg.Devices[0].ID, NodeURL: "https://" + cfg.Listen.Addr, Style: "rich", DataDir: displayconfig.DefaultDataDir,
	}}), false, nil
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
	p := Profile{Edit: edit, SchemaVersion: SchemaVersion, DeviceTokenRef: tokenRef}
	if _, err := m.environment(ctx, p); err != nil {
		return Profile{}, err
	}
	return normalizeProfile(p), nil
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
	env := displayconfig.Environment{NodeURL: p.NodeURL, DeviceID: p.DeviceID, SourceNode: cfg.SourceNode.ID, CertPin: pin, DeviceToken: token, DataDir: p.DataDir, Style: p.Style, PageOrder: p.PageOrder, PageDwellSeconds: p.PageDwellSeconds, HomeOnly: p.HomeOnly, PageSubset: p.PageSubset, PageLock: p.PageLock}
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
		if err == nil && status.Connected && status.Restarts == 0 && advanced {
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

func normalizeProfile(p Profile) Profile {
	settings := displayconfig.PageSettings{
		Style: p.Style, PageOrder: p.PageOrder, PageDwellSeconds: p.PageDwellSeconds,
		HomeOnly: p.HomeOnly, PageSubset: p.PageSubset, PageLock: p.PageLock,
	}
	if normalized, _, err := settings.Resolve(); err == nil {
		p.Style, p.PageOrder, p.PageDwellSeconds = normalized.Style, normalized.PageOrder, normalized.PageDwellSeconds
		p.HomeOnly, p.PageSubset, p.PageLock = normalized.HomeOnly, normalized.PageSubset, normalized.PageLock
	}
	return p
}

func profileHash(p Profile) string {
	raw, _ := json.Marshal(p)
	return fmt.Sprintf("%x", sha256.Sum256(raw))
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

const validateScript = `set -eu
umask 077
candidate=$(mktemp /etc/homepi-display/.environment.candidate.XXXXXX)
trap 'rm -f "$candidate"' EXIT
cat > "$candidate"
chown root:root "$candidate"
chmod 600 "$candidate"
homepi-display config validate --file "$candidate" >/dev/null
printf 'validated\n'`

const applyScript = `set -eu
exec 9</etc/homepi-display
flock -x -w 10 9
env=/etc/homepi-display/environment
previous=/etc/homepi-display/environment.previous
test ! -L "$env"
test -f "$env"
test "$(stat -c %U:%G:%a "$env")" = root:root:600
IFS= read -r expected
if [ "$(sha256sum "$env" | cut -d' ' -f1)" != "$expected" ]; then
  printf 'environment_changed\n'
  exit 0
fi
test ! -L "$previous"
if [ -e "$previous" ]; then
  test -f "$previous"
  test "$(stat -c %U:%G:%a "$previous")" = root:root:600
fi
umask 077
candidate=$(mktemp /etc/homepi-display/.environment.candidate.XXXXXX)
trap 'rm -f "$candidate"' EXIT
backup=$(mktemp /etc/homepi-display/.environment.previous.XXXXXX)
trap 'rm -f "$candidate" "$backup"' EXIT
cat > "$candidate"
chown root:root "$candidate"
chmod 600 "$candidate"
homepi-display config validate --file "$candidate" >/dev/null
cp -p "$env" "$backup"
mv -fT "$backup" "$previous"
mv -fT "$candidate" "$env"
if ! systemctl restart homepi-display.service || ! systemctl is-active --quiet homepi-display.service; then
  printf 'restart_failed\n'
  exit 0
fi
sleep 2
if systemctl is-active --quiet homepi-display.service && [ "$(systemctl show homepi-display.service -p NRestarts --value)" = 0 ]; then
  printf 'applied\n'
else
  printf 'restart_failed\n'
fi`

const rollbackScript = `set -eu
exec 9</etc/homepi-display
flock -x -w 10 9
env=/etc/homepi-display/environment
previous=/etc/homepi-display/environment.previous
test ! -L "$env"
IFS= read -r expected
cmp -s "$env" -
test ! -L "$previous"
test -f "$previous"
test "$(stat -c %U:%G:%a "$previous")" = root:root:600
test "$(sha256sum "$previous" | cut -d' ' -f1)" = "$expected"
umask 077
restore=$(mktemp /etc/homepi-display/.environment.rollback.XXXXXX)
trap 'rm -f "$restore"' EXIT
cp -p "$previous" "$restore"
homepi-display config validate --file "$restore" >/dev/null
mv -fT "$restore" "$env"
cmp -s "$previous" "$env"
systemctl restart homepi-display.service
systemctl is-active --quiet homepi-display.service
printf 'rolled_back\n'`
