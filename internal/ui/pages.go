package ui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
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
		page := Page(strings.ToUpper(strings.TrimSpace(raw)))
		if !valid[page] {
			return RotationConfig{}, fmt.Errorf("unknown page %q", raw)
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
	config         RotationConfig
	page           Page
	index          int
	deadline       time.Time
	preempted      bool
	savedPage      Page
	savedIndex     int
	savedRemaining time.Duration
}

func NewRouter(config RotationConfig, now time.Time) *Router {
	index := 0
	for i, page := range config.Order {
		if page == PageCoding {
			index = i
			break
		}
	}
	return &Router{config: config, page: PageCoding, index: index, deadline: now.Add(config.Dwell[PageCoding])}
}

// Update advances automatic rotation and applies CRIT-only preemption. During
// preemption the original page's remaining dwell is frozen and restored.
func (r *Router) Update(now time.Time, critical map[Page]bool) Page {
	criticalPage, hasCritical := r.firstCritical(critical)
	if r.preempted {
		if hasCritical {
			r.page = criticalPage
			return r.page
		}
		r.preempted = false
		r.page, r.index = r.savedPage, r.savedIndex
		r.deadline = now.Add(r.savedRemaining)
		return r.page
	}
	if hasCritical {
		r.savedPage, r.savedIndex = r.page, r.index
		r.savedRemaining = r.deadline.Sub(now)
		if r.savedRemaining < 0 {
			r.savedRemaining = 0
		}
		r.preempted = true
		r.page = criticalPage
		return r.page
	}
	cycle := time.Duration(0)
	for _, page := range r.config.Order {
		cycle += r.config.Dwell[page]
	}
	if cycle > 0 && now.Sub(r.deadline) >= cycle {
		r.deadline = r.deadline.Add(time.Duration(now.Sub(r.deadline)/cycle) * cycle)
	}
	for !now.Before(r.deadline) {
		r.index = (r.index + 1) % len(r.config.Order)
		r.page = r.config.Order[r.index]
		r.deadline = r.deadline.Add(r.config.Dwell[r.page])
	}
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
	for i, page := range r.config.Order {
		if page == r.page {
			return i + 1, len(r.config.Order)
		}
	}
	return 1, len(r.config.Order)
}

func (r *Router) Dwell() time.Duration { return r.config.Dwell[r.page] }
