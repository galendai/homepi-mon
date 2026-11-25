package ui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Page string

const (
	PageHome     Page = "HOME"
	PageCoding   Page = "CODING"
	PageAPI      Page = "API"
	PageHomeLab  Page = "HOMELAB"
	PageServices Page = "SERVICES"
	PageSystem   Page = "SYSTEM"
)

const (
	DefaultPageOrderText = "CODING,API,HOMELAB,SERVICES,SYSTEM"
	DefaultPageDwellText = "CODING:15,API:15,HOMELAB:15,SERVICES:15,SYSTEM:15"
	MinDwellSeconds      = 5
	MaxDwellSeconds      = 300
)

var allPages = []Page{PageCoding, PageAPI, PageHomeLab, PageServices, PageSystem}

// HomeDwell is the GUI-only landing-page interval. The persisted five-page
// rotation remains unchanged so existing kiosk environment files stay valid.
const HomeDwell = 30 * time.Second

func ParsePage(value string) (Page, error) {
	page := Page(strings.ToUpper(strings.TrimSpace(value)))
	if page == PageHome {
		return page, nil
	}
	for _, allowed := range allPages {
		if page == allowed {
			return page, nil
		}
	}
	return "", fmt.Errorf("unknown page %q", value)
}

type RotationConfig struct {
	Order []Page
	Dwell map[Page]time.Duration
}

func DefaultRotationConfig() RotationConfig {
	config, err := ParseRotationConfig(DefaultPageOrderText, DefaultPageDwellText)
	if err != nil {
		panic(err)
	}
	return config
}

func ParseRotationConfig(orderText, dwellText string) (RotationConfig, error) {
	orderParts := strings.Split(orderText, ",")
	if len(orderParts) != len(allPages) {
		return RotationConfig{}, errors.New("page order must contain exactly five pages")
	}
	valid := make(map[Page]bool, len(allPages))
	for _, page := range allPages {
		valid[page] = true
	}
	order := make([]Page, 0, len(orderParts))
	seen := make(map[Page]bool, len(orderParts))
	for _, raw := range orderParts {
		page, err := ParsePage(raw)
		if err != nil {
			return RotationConfig{}, err
		}
		if seen[page] {
			return RotationConfig{}, fmt.Errorf("duplicate page %q", page)
		}
		seen[page] = true
		order = append(order, page)
	}
	dwell := make(map[Page]time.Duration, len(allPages))
	parts := strings.Split(dwellText, ",")
	if len(parts) != len(allPages) {
		return RotationConfig{}, errors.New("page dwell must contain exactly five page values")
	}
	for _, raw := range parts {
		name, secondsText, ok := strings.Cut(raw, ":")
		page := Page(strings.ToUpper(strings.TrimSpace(name)))
		if !ok || !valid[page] {
			return RotationConfig{}, fmt.Errorf("invalid page dwell %q", raw)
		}
		if _, exists := dwell[page]; exists {
			return RotationConfig{}, fmt.Errorf("duplicate page dwell %q", page)
		}
		seconds, err := strconv.Atoi(strings.TrimSpace(secondsText))
		if err != nil || seconds < MinDwellSeconds || seconds > MaxDwellSeconds {
			return RotationConfig{}, fmt.Errorf("page %s dwell must be %d..%d seconds", page, MinDwellSeconds, MaxDwellSeconds)
		}
		dwell[page] = time.Duration(seconds) * time.Second
	}
	return RotationConfig{Order: order, Dwell: dwell}, nil
}

// SelectRotationPages builds a runtime-only subset while preserving the
// caller's persistent five-page configuration and per-page dwell values.
func SelectRotationPages(config RotationConfig, text string) (RotationConfig, error) {
	parts := strings.Split(text, ",")
	selected := RotationConfig{
		Order: make([]Page, 0, len(parts)),
		Dwell: make(map[Page]time.Duration, len(parts)),
	}
	seen := make(map[Page]bool, len(parts))
	for _, raw := range parts {
		page, err := ParsePage(raw)
		if err != nil {
			return RotationConfig{}, err
		}
		if seen[page] {
			return RotationConfig{}, fmt.Errorf("duplicate page %q", page)
		}
		dwell := HomeDwell
		if page != PageHome {
			var ok bool
			dwell, ok = config.Dwell[page]
			if !ok {
				return RotationConfig{}, fmt.Errorf("page %s has no configured dwell", page)
			}
		}
		seen[page] = true
		selected.Order = append(selected.Order, page)
		selected.Dwell[page] = dwell
	}
	return selected, nil
}

func (c RotationConfig) Strings() (string, string) {
	order := make([]string, len(c.Order))
	dwell := make([]string, len(c.Order))
	for i, page := range c.Order {
		order[i] = string(page)
		dwell[i] = fmt.Sprintf("%s:%d", page, int(c.Dwell[page]/time.Second))
	}
	return strings.Join(order, ","), strings.Join(dwell, ",")
}

type Router struct {
	mu sync.Mutex

	config    RotationConfig
	page      Page
	index     int
	remaining time.Duration
	last      time.Time
	paused    bool

	remotePage  Page
	remoteUntil time.Time

	rotationOverride bool
	rotationEnabled  bool
	rotationInterval time.Duration
	rotationUntil    time.Time
	pageOnly         bool
	displayed        Page
}

func NewRouter(config RotationConfig, now time.Time) *Router {
	initial := PageCoding
	if len(config.Order) > 0 {
		initial = config.Order[0]
		for _, page := range config.Order {
			if page == PageCoding {
				initial = PageCoding
				break
			}
		}
	}
	return newRouter(config, initial, now)
}

// NewSubsetRouter starts a runtime-selected page list at its first entry,
// including HOME when explicitly selected.
func NewSubsetRouter(config RotationConfig, now time.Time) *Router {
	initial := PageCoding
	if len(config.Order) > 0 {
		initial = config.Order[0]
	}
	return newRouter(config, initial, now)
}

// NewHomeRouter prepends the pixel landing page without mutating the caller's
// persisted five-page configuration.
func NewHomeRouter(config RotationConfig, now time.Time) *Router {
	homeConfig := RotationConfig{
		Order: make([]Page, 0, len(config.Order)+1),
		Dwell: make(map[Page]time.Duration, len(config.Dwell)+1),
	}
	homeConfig.Order = append(homeConfig.Order, PageHome)
	homeConfig.Order = append(homeConfig.Order, config.Order...)
	homeConfig.Dwell[PageHome] = HomeDwell
	for page, dwell := range config.Dwell {
		homeConfig.Dwell[page] = dwell
	}
	return newRouter(homeConfig, PageHome, now)
}

func newRouter(config RotationConfig, initial Page, now time.Time) *Router {
	index := 0
	for i, page := range config.Order {
		if page == initial {
			index = i
			break
		}
	}
	return &Router{
		config: config, page: initial, index: index,
		remaining: config.Dwell[initial], last: now,
		rotationEnabled: true, displayed: initial,
	}
}

// Update advances automatic rotation. Only remote page overrides and explicit
// rotation pauses freeze the base page's remaining dwell.
func (r *Router) Update(now time.Time) Page {
	r.mu.Lock()
	defer r.mu.Unlock()
	if now.Before(r.last) {
		r.last = now
	}
	elapsed := now.Sub(r.last)
	if !r.paused {
		r.advanceLocked(elapsed)
	}
	r.last = now
	if !r.remoteUntil.IsZero() && !now.Before(r.remoteUntil) {
		r.remotePage, r.remoteUntil = "", time.Time{}
	}
	if r.rotationOverride && !now.Before(r.rotationUntil) {
		r.rotationOverride = false
		r.rotationEnabled = true
		r.rotationInterval = 0
		if r.remaining > r.config.Dwell[r.page] {
			r.remaining = r.config.Dwell[r.page]
		}
	}
	if r.remotePage != "" {
		r.paused = true
		r.displayed = r.remotePage
		return r.remotePage
	}
	r.paused = r.pageOnly || r.rotationOverride && !r.rotationEnabled
	r.displayed = r.page
	return r.page
}

func (r *Router) Position() (int, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	page := r.displayed
	for i, candidate := range r.config.Order {
		if candidate == page {
			return i + 1, len(r.config.Order)
		}
	}
	return 1, len(r.config.Order)
}

// SetHomeOnly disables automatic rotation until an explicit remote rotation
// command changes it. Temporary page overrides still work and expire to HOME.
func (r *Router) SetHomeOnly(now time.Time) {
	r.SetPageOnly(PageHome, now)
}

// SetPageOnly holds one configured page until an explicit rotation command.
// Temporary remote pages still work and expire back to the selected page.
func (r *Router) SetPageOnly(page Page, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	index := 0
	for candidateIndex, candidate := range r.config.Order {
		if candidate == page {
			index = candidateIndex
			break
		}
	}
	r.page, r.index, r.remaining = page, index, r.config.Dwell[page]
	r.last, r.paused, r.displayed = now, true, page
	r.pageOnly, r.rotationEnabled = true, false
	r.rotationOverride, r.rotationInterval, r.rotationUntil = false, 0, time.Time{}
	r.remotePage, r.remoteUntil = "", time.Time{}
}

func (r *Router) Dwell() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.dwellLocked(r.page)
}

func (r *Router) RotationEnabled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.pageOnly && (!r.rotationOverride || r.rotationEnabled)
}

func (r *Router) ShowPage(page Page, duration time.Duration, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.freezeLocked(now)
	r.remotePage = page
	r.remoteUntil = now.Add(duration)
}

func (r *Router) NextPage(duration time.Duration, now time.Time) {
	r.showStepLocked(1, duration, now)
}

func (r *Router) PreviousPage(duration time.Duration, now time.Time) {
	r.showStepLocked(-1, duration, now)
}

func (r *Router) showStepLocked(step int, duration time.Duration, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.freezeLocked(now)
	base := r.index
	if r.remotePage != "" {
		for i, page := range r.config.Order {
			if page == r.remotePage {
				base = i
				break
			}
		}
	}
	base = (base + step + len(r.config.Order)) % len(r.config.Order)
	r.remotePage = r.config.Order[base]
	r.remoteUntil = now.Add(duration)
}

func (r *Router) SetRotation(enabled bool, interval, duration time.Duration, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pageOnly = false
	if now.After(r.last) && !r.paused {
		r.advanceLocked(now.Sub(r.last))
	}
	r.last = now
	r.rotationOverride = true
	r.rotationEnabled = enabled
	r.rotationInterval = interval
	r.rotationUntil = now.Add(duration)
	if enabled && interval > 0 {
		r.remaining = interval
	}
	r.paused = r.remotePage != "" || !enabled
}

func (r *Router) freezeLocked(now time.Time) {
	if now.After(r.last) && !r.paused {
		r.advanceLocked(now.Sub(r.last))
	}
	r.last = now
	r.paused = true
}

func (r *Router) advanceLocked(elapsed time.Duration) {
	if elapsed <= 0 {
		return
	}
	for elapsed >= r.remaining {
		elapsed -= r.remaining
		r.index = (r.index + 1) % len(r.config.Order)
		r.page = r.config.Order[r.index]
		r.remaining = r.dwellLocked(r.page)
	}
	r.remaining -= elapsed
}

func (r *Router) dwellLocked(page Page) time.Duration {
	if r.rotationOverride && r.rotationEnabled && r.rotationInterval > 0 {
		return r.rotationInterval
	}
	return r.config.Dwell[page]
}
