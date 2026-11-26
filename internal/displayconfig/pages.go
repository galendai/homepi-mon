package displayconfig

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/galendai/homepi-mon/internal/ui"
)

// PageSettings contains only settings that may be shown to an operator.
type PageSettings struct {
	Style            string `json:"style"`
	PageOrder        string `json:"page_order"`
	PageDwellSeconds string `json:"page_dwell_seconds"`
	HomeOnly         string `json:"home_only"`
	PageSubset       string `json:"page_subset,omitempty"`
	PageLock         string `json:"page_lock,omitempty"`
}

// WithFlags applies explicitly supplied flags. A mode flag replaces the
// entire environment mode, including a legacy HOME-only setting.
func (s PageSettings) WithFlags(flags map[string]string) PageSettings {
	_, pages := flags["pages"]
	_, lock := flags["page-only"]
	_, home := flags["home-only"]
	if pages || lock || home {
		s.PageSubset, s.PageLock, s.HomeOnly = flags["pages"], flags["page-only"], flags["home-only"]
	}
	if v, ok := flags["page-order"]; ok {
		s.PageOrder = v
	}
	if v, ok := flags["page-dwell-seconds"]; ok {
		s.PageDwellSeconds = v
	}
	if v, ok := flags["style"]; ok {
		s.Style = v
	}
	return s
}

// Resolve returns canonical file settings and the rotation used by the router.
func (s PageSettings) Resolve() (PageSettings, ui.RotationConfig, error) {
	if s.PageOrder == "" {
		s.PageOrder = ui.DefaultPageOrderText
	}
	if s.PageDwellSeconds == "" {
		s.PageDwellSeconds = ui.DefaultPageDwellText
	}
	if s.HomeOnly == "" {
		s.HomeOnly = "false"
	}
	style, err := ui.ParseStyle(s.Style)
	if err != nil {
		return s, ui.RotationConfig{}, err
	}
	s.Style = string(style)
	rotation, err := ui.ParseRotationConfig(s.PageOrder, s.PageDwellSeconds)
	if err != nil {
		return s, rotation, err
	}
	s.PageOrder, _ = rotation.Strings()
	// Dwell is a map; its canonical text is independent of the selected order.
	dwell := ui.DefaultRotationConfig()
	dwell.Dwell = rotation.Dwell
	_, s.PageDwellSeconds = dwell.Strings()
	home, err := strconv.ParseBool(s.HomeOnly)
	if err != nil {
		return s, rotation, errors.New("home-only must be true or false")
	}
	s.HomeOnly = strconv.FormatBool(home)
	if (home && (s.PageLock != "" || s.PageSubset != "")) || (s.PageLock != "" && s.PageSubset != "") {
		return s, rotation, errors.New("pages, page-only, and enabled home-only are mutually exclusive")
	}
	if s.PageLock != "" {
		page, err := ui.ParsePage(s.PageLock)
		if err != nil {
			return s, rotation, fmt.Errorf("page-only: %w", err)
		}
		s.PageLock = string(page)
		// OPENCLASH is absent from both default rotations.
		if page == ui.PageOpenClash {
			rotation, err = ui.SelectRotationPages(rotation, s.PageLock)
			return s, rotation, err
		}
	}
	if s.PageSubset != "" {
		rotation, err = ui.SelectRotationPages(rotation, s.PageSubset)
		if err != nil {
			return s, rotation, fmt.Errorf("pages: %w", err)
		}
		s.PageSubset, _ = rotation.Strings()
	}
	return s, rotation, nil
}
