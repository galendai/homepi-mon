// Package displayconfig owns the validated environment contract shared by
// homepi-display and the Phase 2 SSH deployer.
package displayconfig

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/galendai/homepi-mon/internal/tlsconfig"
	"github.com/galendai/homepi-mon/internal/ui"
)

const DefaultDataDir = "/var/lib/homepi-display"

var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

var allowedKeys = map[string]bool{
	"HOMEPI_NODE_URL":           true,
	"HOMEPI_DEVICE_ID":          true,
	"HOMEPI_SOURCE_NODE_ID":     true,
	"HOMEPI_NODE_CERT_PIN":      true,
	"HOMEPI_DEVICE_TOKEN":       true,
	"HOMEPI_DISPLAY_DATA_DIR":   true,
	"HOMEPI_DISPLAY_STYLE":      true,
	"HOMEPI_PAGE_ORDER":         true,
	"HOMEPI_PAGE_DWELL_SECONDS": true,
}

var orderedKeys = []string{
	"HOMEPI_NODE_URL",
	"HOMEPI_DEVICE_ID",
	"HOMEPI_SOURCE_NODE_ID",
	"HOMEPI_NODE_CERT_PIN",
	"HOMEPI_DEVICE_TOKEN",
	"HOMEPI_DISPLAY_DATA_DIR",
	"HOMEPI_DISPLAY_STYLE",
	"HOMEPI_PAGE_ORDER",
	"HOMEPI_PAGE_DWELL_SECONDS",
}

// Environment contains the only values accepted by the kiosk environment
// file. The token must never be logged or returned through the Web API.
type Environment struct {
	NodeURL          string
	DeviceID         string
	SourceNode       string
	CertPin          string
	DeviceToken      string
	DataDir          string
	Style            string
	PageOrder        string
	PageDwellSeconds string
}

func (e Environment) Validate() error {
	e = e.withDefaults()
	values := e.values()
	for _, key := range orderedKeys {
		value := values[key]
		if value == "" {
			return fmt.Errorf("display config: %s is required", key)
		}
		if err := validateValue(value); err != nil {
			return fmt.Errorf("display config: %s: %w", key, err)
		}
	}
	if !safeID.MatchString(e.DeviceID) {
		return errors.New("display config: HOMEPI_DEVICE_ID is invalid")
	}
	if !safeID.MatchString(e.SourceNode) {
		return errors.New("display config: HOMEPI_SOURCE_NODE_ID is invalid")
	}
	u, err := url.Parse(e.NodeURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("display config: HOMEPI_NODE_URL must be an https origin")
	}
	if u.Path != "" && u.Path != "/" {
		return errors.New("display config: HOMEPI_NODE_URL must not contain a path")
	}
	if _, err := tlsconfig.ParseFingerprint(e.CertPin); err != nil {
		return fmt.Errorf("display config: HOMEPI_NODE_CERT_PIN: %w", err)
	}
	if e.DataDir != DefaultDataDir {
		return fmt.Errorf("display config: HOMEPI_DISPLAY_DATA_DIR must be %s", DefaultDataDir)
	}
	if _, err := ui.ParseStyle(e.Style); err != nil {
		return fmt.Errorf("display config: HOMEPI_DISPLAY_STYLE: %w", err)
	}
	if _, err := ui.ParseRotationConfig(e.PageOrder, e.PageDwellSeconds); err != nil {
		return fmt.Errorf("display config: page rotation: %w", err)
	}
	return nil
}

// Render returns a deterministic systemd EnvironmentFile payload. Values are
// deliberately unquoted because validation rejects every character that could
// require shell or systemd escaping.
func (e Environment) Render() ([]byte, error) {
	e = e.withDefaults()
	if err := e.Validate(); err != nil {
		return nil, err
	}
	values := e.values()
	var b strings.Builder
	for _, key := range orderedKeys {
		fmt.Fprintf(&b, "%s=%s\n", key, values[key])
	}
	return []byte(b.String()), nil
}

func (e Environment) withDefaults() Environment {
	if e.PageOrder == "" {
		e.PageOrder = ui.DefaultPageOrderText
	}
	if e.PageDwellSeconds == "" {
		e.PageDwellSeconds = ui.DefaultPageDwellText
	}
	return e
}

func (e Environment) values() map[string]string {
	return map[string]string{
		"HOMEPI_NODE_URL":           e.NodeURL,
		"HOMEPI_DEVICE_ID":          e.DeviceID,
		"HOMEPI_SOURCE_NODE_ID":     e.SourceNode,
		"HOMEPI_NODE_CERT_PIN":      e.CertPin,
		"HOMEPI_DEVICE_TOKEN":       e.DeviceToken,
		"HOMEPI_DISPLAY_DATA_DIR":   e.DataDir,
		"HOMEPI_DISPLAY_STYLE":      e.Style,
		"HOMEPI_PAGE_ORDER":         e.PageOrder,
		"HOMEPI_PAGE_DWELL_SECONDS": e.PageDwellSeconds,
	}
}

// Parse reads the strict EnvironmentFile subset emitted by Render.
func Parse(r io.Reader) (Environment, error) {
	values := make(map[string]string, len(orderedKeys))
	s := bufio.NewScanner(io.LimitReader(r, 64<<10))
	for s.Scan() {
		line := s.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || !allowedKeys[key] {
			return Environment{}, errors.New("display config: unknown or malformed environment key")
		}
		if _, exists := values[key]; exists {
			return Environment{}, fmt.Errorf("display config: duplicate key %s", key)
		}
		if err := validateValue(value); err != nil {
			return Environment{}, fmt.Errorf("display config: %s: %w", key, err)
		}
		values[key] = value
	}
	if err := s.Err(); err != nil {
		return Environment{}, fmt.Errorf("display config: read: %w", err)
	}
	e := Environment{
		NodeURL: values["HOMEPI_NODE_URL"], DeviceID: values["HOMEPI_DEVICE_ID"],
		SourceNode: values["HOMEPI_SOURCE_NODE_ID"], CertPin: values["HOMEPI_NODE_CERT_PIN"],
		DeviceToken: values["HOMEPI_DEVICE_TOKEN"], DataDir: values["HOMEPI_DISPLAY_DATA_DIR"],
		Style:     values["HOMEPI_DISPLAY_STYLE"],
		PageOrder: values["HOMEPI_PAGE_ORDER"], PageDwellSeconds: values["HOMEPI_PAGE_DWELL_SECONDS"],
	}
	if e.PageOrder == "" {
		e.PageOrder = ui.DefaultPageOrderText
	}
	if e.PageDwellSeconds == "" {
		e.PageDwellSeconds = ui.DefaultPageDwellText
	}
	if err := e.Validate(); err != nil {
		return Environment{}, err
	}
	return e, nil
}

func validateValue(value string) error {
	if len(value) > 4096 {
		return errors.New("value is too long")
	}
	if strings.ContainsAny(value, "\x00\r\n`$") {
		return errors.New("value contains a forbidden control or expansion character")
	}
	if strings.TrimSpace(value) != value || strings.ContainsAny(value, "\t ") {
		return errors.New("value contains whitespace")
	}
	return nil
}

// Keys returns the stable allowlist for tests and diagnostics.
func Keys() []string {
	keys := append([]string(nil), orderedKeys...)
	sort.Strings(keys)
	return keys
}
