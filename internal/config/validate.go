package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

// Validate enforces every invariant the daemon needs before it can
// listen. It returns the first violation so the operator gets a
// deterministic error message.
//
// Recognised provider types are passed in knownTypes so this package
// does not have to know about the connector registry. The registry
// typically passes a small static list.
func (c *Config) Validate() error {
	if c.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schema_version=%d, want %d", c.SchemaVersion, SchemaVersion)
	}
	if err := c.validateSourceNode(); err != nil {
		return err
	}
	if err := c.validateListen(); err != nil {
		return err
	}
	if err := c.validateDevices(); err != nil {
		return err
	}
	if err := c.validateProviders(); err != nil {
		return err
	}
	return nil
}

// ValidateWithRegistry is like Validate but additionally checks that
// every Provider.Type is in knownTypes. The registry wires this in at
// serve time; other callers can rely on the basic Validate.
func (c *Config) ValidateWithRegistry(knownTypes map[string]bool) error {
	if err := c.Validate(); err != nil {
		return err
	}
	for i, p := range c.Providers {
		if !knownTypes[p.Type] {
			return fmt.Errorf("providers[%d] type=%q is not registered", i, p.Type)
		}
	}
	return nil
}

func (c *Config) validateSourceNode() error {
	if !safeID(c.SourceNode.ID) {
		return fmt.Errorf("source_node.id %q must be 1..64 chars of [A-Za-z0-9._-]", c.SourceNode.ID)
	}
	if c.SourceNode.Label != "" && len(c.SourceNode.Label) > 64 {
		return fmt.Errorf("source_node.label too long: %d", len(c.SourceNode.Label))
	}
	return nil
}

func (c *Config) validateListen() error {
	host, port, err := splitHostPort(c.Listen.Addr)
	if err != nil {
		return fmt.Errorf("listen.addr %q: %w", c.Listen.Addr, err)
	}
	if !c.Listen.AllowPublicBind {
		if isPublicHost(host) {
			return fmt.Errorf("listen.addr %q binds to a public interface; "+
				"set listen.allow_public_bind=true to acknowledge", c.Listen.Addr)
		}
	}
	if port == "" {
		return fmt.Errorf("listen.addr %q: missing port", c.Listen.Addr)
	}
	cert := c.Listen.TLS.Cert
	key := c.Listen.TLS.Key
	if (cert == "") != (key == "") {
		return errors.New("listen.tls.cert and listen.tls.key must be configured together")
	}
	if (cert == "auto") != (key == "auto") {
		return errors.New("listen.tls.cert and listen.tls.key must both be auto or explicit paths")
	}
	if c.Listen.TLS.Cert != "" && c.Listen.TLS.Cert != "auto" {
		if !strings.HasPrefix(c.Listen.TLS.Cert, "/") &&
			!strings.HasPrefix(c.Listen.TLS.Cert, "./") {
			return fmt.Errorf("listen.tls.cert must be an absolute path, \"./\" or \"auto\"")
		}
	}
	if c.Listen.TLS.Key != "" && c.Listen.TLS.Key != "auto" {
		if !strings.HasPrefix(c.Listen.TLS.Key, "/") &&
			!strings.HasPrefix(c.Listen.TLS.Key, "./") {
			return fmt.Errorf("listen.tls.key must be an absolute path, \"./\" or \"auto\"")
		}
	}
	return nil
}

func (c *Config) validateDevices() error {
	seen := make(map[string]bool)
	for i, d := range c.Devices {
		if !safeID(d.ID) {
			return fmt.Errorf("devices[%d].id %q invalid", i, d.ID)
		}
		if seen[d.ID] {
			return fmt.Errorf("devices[%d] duplicate id %q", i, d.ID)
		}
		seen[d.ID] = true
		if err := validateTokenRef(d.TokenRef); err != nil {
			return fmt.Errorf("devices[%d]: %w", i, err)
		}
	}
	return nil
}

func (c *Config) validateProviders() error {
	seen := make(map[string]bool)
	for i, p := range c.Providers {
		if !safeID(p.ID) {
			return fmt.Errorf("providers[%d].id %q invalid", i, p.ID)
		}
		if seen[p.ID] {
			return fmt.Errorf("providers[%d] duplicate id %q", i, p.ID)
		}
		seen[p.ID] = true
		if !safeID(p.Type) {
			return fmt.Errorf("providers[%d].type %q invalid", i, p.Type)
		}
		if p.AccountLabel == "" {
			return fmt.Errorf("providers[%d].account_label is required", i)
		}
		if err := validateRegion(p); err != nil {
			return fmt.Errorf("providers[%d]: %w", i, err)
		}
		interval, err := p.IntervalDuration()
		if err != nil {
			return err
		}
		stale, err := p.StaleAfterDuration()
		if err != nil {
			return err
		}
		if interval < 5*time.Second {
			return fmt.Errorf("providers[%d].interval=%s must be >=5s", i, interval)
		}
		if stale < interval {
			return fmt.Errorf("providers[%d].stale_after=%s must be >= interval=%s",
				i, stale, interval)
		}
		if err := validateOptionalTokenRef(p.SecretRef); err != nil {
			return fmt.Errorf("providers[%d]: %w", i, err)
		}
	}
	return nil
}

func validateRegion(p ProviderConfig) error {
	switch p.Region {
	case "global", "cn":
		if p.BaseURL != "" {
			return fmt.Errorf("region=%s must not set base_url", p.Region)
		}
	case "custom":
		if p.BaseURL == "" {
			return errors.New("region=custom requires base_url")
		}
		u, err := url.Parse(p.BaseURL)
		if err != nil {
			return fmt.Errorf("base_url: %w", err)
		}
		if u.Scheme != "https" && u.Scheme != "http" {
			return fmt.Errorf("base_url scheme %q must be http or https", u.Scheme)
		}
		if u.Host == "" {
			return errors.New("base_url is missing a host")
		}
		if err := rejectSSRF(u.Hostname()); err != nil {
			return fmt.Errorf("base_url: %w", err)
		}
		if u.Scheme == "http" && !isLoopbackHost(u.Hostname()) {
			return fmt.Errorf("base_url uses http on a non-loopback host; use https or 127.0.0.1")
		}
	default:
		return fmt.Errorf("region=%q must be global|cn|custom", p.Region)
	}
	return nil
}

func validateTokenRef(ref string) error {
	if ref == "" {
		return errors.New("token_ref is required")
	}
	const prefix = "keyring:"
	if !strings.HasPrefix(ref, prefix) {
		return fmt.Errorf("token_ref %q must start with %q", ref, prefix)
	}
	name := strings.TrimPrefix(ref, prefix)
	if name == "" {
		return fmt.Errorf("token_ref %q has empty name", ref)
	}
	if strings.ContainsAny(name, " \t\n\r") {
		return fmt.Errorf("token_ref %q contains whitespace", ref)
	}
	return nil
}

// validateOptionalTokenRef is the provider variant: an empty ref is
// allowed (mock) but a non-empty ref must still be shaped correctly.
func validateOptionalTokenRef(ref string) error {
	if ref == "" {
		return nil
	}
	return validateTokenRef(ref)
}

func rejectSSRF(host string) error {
	ip := net.ParseIP(host)
	if ip == nil {
		// A hostname is acceptable; we cannot resolve it here without
		// touching DNS, and the daemon does that at request time.
		return nil
	}
	if ip.IsLoopback() {
		return nil
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("host %s is a link-local address", host)
	}
	if ip.IsPrivate() {
		// Allow only loopback within the private space; reject RFC1918
		// because Phase 1 connects to public Provider APIs, not the LAN.
		return fmt.Errorf("host %s is a private address; "+
			"this version rejects RFC1918 hosts to prevent SSRF", host)
	}
	// Well-known cloud metadata endpoints.
	if ip.Equal(metadataIP) {
		return fmt.Errorf("host %s is the cloud metadata endpoint", host)
	}
	return nil
}

var metadataIP = net.ParseIP("169.254.169.254")

func safeID(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return false
		}
	}
	return true
}

func splitHostPort(addr string) (string, string, error) {
	if !strings.Contains(addr, ":") {
		return "", "", fmt.Errorf("missing port")
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", "", err
	}
	return host, port, nil
}

func isPublicHost(host string) bool {
	if host == "" {
		return false
	}
	if isLoopbackHost(host) {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// A non-IP host (DNS name): assume public unless it resolves
		// to a loopback. Daemon startup already prints the resolved IP,
		// so the operator sees a clear warning here.
		return true
	}
	if ip.IsPrivate() || ip.IsLinkLocalUnicast() {
		return false
	}
	return true
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func parseDuration(field, value string) (time.Duration, error) {
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s=%q: %w", field, value, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s=%q must be positive", field, value)
	}
	return d, nil
}
