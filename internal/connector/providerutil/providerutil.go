// Package providerutil contains the small, security-sensitive primitives
// shared by the Phase 1 provider connectors.
package providerutil

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/galendai/homepi-mon/internal/config"
	"github.com/galendai/homepi-mon/internal/connector"
	"github.com/galendai/homepi-mon/internal/decimal"
	"github.com/galendai/homepi-mon/internal/protocol"
	"github.com/galendai/homepi-mon/internal/secretstore"
)

// Runtime dependencies are injectable so connector tests use local servers
// and deterministic clocks without weakening production validation.
type Runtime struct {
	HTTPClient *http.Client
	Now        func() time.Time
}

// NormalizeRuntime fills production defaults.
func NormalizeRuntime(r Runtime) Runtime {
	if r.Now == nil {
		r.Now = time.Now
	}
	return r
}

// BaseURL selects a fixed regional endpoint or the explicitly validated
// custom endpoint.
func BaseURL(spec config.ProviderConfig, global, cn string) (string, error) {
	var raw string
	switch spec.Region {
	case "global":
		raw = global
	case "cn":
		raw = cn
	case "custom":
		raw = spec.BaseURL
	default:
		return "", connector.Errorf(protocol.ErrInvalidConfig, "provider region is invalid")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", connector.Errorf(protocol.ErrInvalidConfig, "provider base URL is invalid")
	}
	return strings.TrimRight(raw, "/"), nil
}

// Secret resolves a provider key lazily for each collection.
func Secret(ctx context.Context, spec config.ProviderConfig, store secretstore.Store) (string, error) {
	if spec.SecretRef == "" || store == nil {
		return "", connector.Errorf(protocol.ErrInvalidConfig, "provider credential reference is missing")
	}
	value, err := store.Get(ctx, spec.SecretRef)
	if err != nil {
		if errors.Is(err, secretstore.ErrNotFound) {
			return "", connector.Errorf(protocol.ErrAuth, "provider credential is not configured")
		}
		return "", connector.Errorf(protocol.ErrAuth, "provider credential could not be read")
	}
	if strings.TrimSpace(value) == "" {
		return "", connector.Errorf(protocol.ErrAuth, "provider credential is empty")
	}
	return value, nil
}

// Decimal parses a JSON string or number without binary floating point.
func Decimal(v any) (decimal.Decimal, error) {
	switch value := v.(type) {
	case string:
		return decimal.Parse(value)
	case json.Number:
		return decimal.Parse(value.String())
	case nil:
		return decimal.Decimal{}, errors.New("value is missing")
	default:
		return decimal.Parse(fmt.Sprint(value))
	}
}

// Remaining returns an explicit remaining value when present, otherwise
// derives total-used with exact decimal arithmetic.
func Remaining(total, used, remaining any) (decimal.Decimal, decimal.Decimal, []string, error) {
	limit, err := Decimal(total)
	if err != nil || limit.Cmp(decimal.Decimal{}) <= 0 {
		return decimal.Decimal{}, decimal.Decimal{}, nil, errors.New("limit is missing or invalid")
	}
	if remaining != nil {
		left, err := Decimal(remaining)
		if err != nil || left.Cmp(decimal.Decimal{}) < 0 || left.Cmp(limit) > 0 {
			return decimal.Decimal{}, decimal.Decimal{}, nil, errors.New("remaining is invalid")
		}
		return left, limit, nil, nil
	}
	consumed, err := Decimal(used)
	if err != nil || consumed.Cmp(decimal.Decimal{}) < 0 || consumed.Cmp(limit) > 0 {
		return decimal.Decimal{}, decimal.Decimal{}, nil, errors.New("used is missing or invalid")
	}
	left, err := limit.Sub(consumed)
	if err != nil {
		return decimal.Decimal{}, decimal.Decimal{}, nil, err
	}
	return left, limit, []string{"remaining"}, nil
}

// ResetTime accepts Unix seconds/milliseconds, an RFC3339 string or a
// relative number of seconds. Unknown reset data is omitted.
func ResetTime(value any, relativeSeconds any, now time.Time) *time.Time {
	if value != nil {
		var parsed time.Time
		switch raw := value.(type) {
		case string:
			if t, err := time.Parse(time.RFC3339, raw); err == nil {
				parsed = t
			} else if n, err := json.Number(raw).Int64(); err == nil {
				parsed = unixTime(n)
			}
		case json.Number:
			if n, err := raw.Int64(); err == nil {
				parsed = unixTime(n)
			}
		case float64:
			parsed = unixTime(int64(raw))
		case int64:
			parsed = unixTime(raw)
		case int:
			parsed = unixTime(int64(raw))
		}
		if !parsed.IsZero() {
			t := parsed.UTC()
			return &t
		}
	}
	if relativeSeconds != nil {
		seconds, err := Decimal(relativeSeconds)
		if err == nil {
			if whole, err := time.ParseDuration(seconds.String() + "s"); err == nil && whole > 0 {
				t := now.UTC().Add(whole)
				return &t
			}
		}
	}
	return nil
}

func unixTime(v int64) time.Time {
	if v > 1_000_000_000_000 {
		return time.UnixMilli(v)
	}
	return time.Unix(v, 0)
}

// ExpandAuthFile resolves the Codex default and ~/ prefix without reading it.
func ExpandAuthFile(raw string) (string, error) {
	if raw == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", connector.Errorf(protocol.ErrInvalidConfig, "locate current user home directory")
		}
		return filepath.Join(home, ".codex", "auth.json"), nil
	}
	if strings.HasPrefix(raw, "~/") || strings.HasPrefix(raw, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", connector.Errorf(protocol.ErrInvalidConfig, "locate current user home directory")
		}
		return filepath.Join(home, raw[2:]), nil
	}
	if !filepath.IsAbs(raw) {
		return "", connector.Errorf(protocol.ErrInvalidConfig, "auth_file must be absolute or start with ~/")
	}
	return filepath.Clean(raw), nil
}
