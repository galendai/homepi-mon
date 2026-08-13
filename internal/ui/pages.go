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

func ParsePage(value string) (Page, error) {
	page := Page(strings.ToUpper(strings.TrimSpace(value)))
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
	displayed        Page
	criticalActive   bool
}

func NewRouter(config RotationConfig, now time.Time) *Router {
	index := 0
	for i, page := range config.Order {
		if page == PageCoding {
			index = i
			break
		}
	}
	return &Router{
		config: config, page: PageCoding, index: index,
		remaining: config.Dwell[PageCoding], last: now,
		rotationEnabled: true, displayed: PageCoding,
	}
}

// Update advances automatic rotation and applies CRIT-only preemption. During
// critical or remote preemption the base page's remaining dwell is frozen.
func (r *Router) Update(now time.Time, critical map[Page]bool) Page {
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
	criticalPage, hasCritical := r.firstCritical(critical)
	if hasCritical {
		r.paused = true
		r.displayed = criticalPage
		r.criticalActive = true
		return criticalPage
	}
	r.criticalActive = false
	if r.remotePage != "" {
		r.paused = true
		r.displayed = r.remotePage
		return r.remotePage
	}
	r.paused = r.rotationOverride && !r.rotationEnabled
	r.displayed = r.page
	return r.page
}

func (r *Router) firstCritical(critical map[Page]bool) (Page, bool) {
	for _, page := range r.config.Order {
		if critical[page] {
			return page, true
		}
	}
	return "", false
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

func (r *Router) Dwell() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.dwellLocked(r.page)
}

func (r *Router) RotationEnabled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.rotationOverride || r.rotationEnabled
}

func (r *Router) CriticalActive() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.criticalActive
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
