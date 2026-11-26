package protocol

import (
	"errors"
	"fmt"
	"time"
)

// MaxOpenClashNodes bounds the proxy data retained in every snapshot.
const MaxOpenClashNodes = 64

// OpenClashReport is the current, deliberately credential-free view of one
// OpenClash controller. Delay values come only from checks performed by this
// daemon; upstream history samples are never treated as current readings.
type OpenClashReport struct {
	ObservedAt       time.Time       `json:"observed_at"`
	StaleAfter       Duration        `json:"stale_after"`
	DelayStaleAfter  Duration        `json:"delay_stale_after"`
	SelectedGroup    string          `json:"selected_group"`
	SelectedNode     string          `json:"selected_node"`
	TotalConnections int             `json:"total_connections"`
	Nodes            []OpenClashNode `json:"nodes"`
	Status           MetricStatus    `json:"status"`
	ErrorClass       ErrorClass      `json:"error_class,omitempty"`
	Message          string          `json:"message,omitempty"`
}

// OpenClashNode contains only display-ready proxy identity and measurements.
type OpenClashNode struct {
	Name              string     `json:"name"`
	Alive             *bool      `json:"alive,omitempty"`
	DelayMS           *int       `json:"delay_ms,omitempty"`
	CheckedAt         *time.Time `json:"checked_at,omitempty"`
	ActiveConnections int        `json:"active_connections"`
}

// Clone returns a deep copy that shares no node or measurement pointers.
func (r OpenClashReport) Clone() OpenClashReport {
	out := r
	out.Nodes = make([]OpenClashNode, len(r.Nodes))
	for i, node := range r.Nodes {
		out.Nodes[i] = node
		out.Nodes[i].Alive = cloneBool(node.Alive)
		out.Nodes[i].DelayMS = cloneInt(node.DelayMS)
		if node.CheckedAt != nil {
			checkedAt := *node.CheckedAt
			out.Nodes[i].CheckedAt = &checkedAt
		}
	}
	return out
}

// Validate checks bounds, uniqueness and terminal-safe text.
func (r *OpenClashReport) Validate() error {
	if r == nil {
		return errors.New("openclash: nil")
	}
	if r.ObservedAt.IsZero() {
		return errors.New("observed_at: must be set")
	}
	if r.StaleAfter.D() < 0 {
		return errors.New("stale_after: must not be negative")
	}
	if r.DelayStaleAfter.D() < 0 {
		return errors.New("delay_stale_after: must not be negative")
	}
	if err := safeText("selected_group", r.SelectedGroup, 128); err != nil {
		return err
	}
	if r.SelectedGroup == "" {
		return errors.New("selected_group: must not be empty")
	}
	if err := safeText("selected_node", r.SelectedNode, 128); err != nil {
		return err
	}
	if r.TotalConnections < 0 {
		return errors.New("total_connections: must not be negative")
	}
	if len(r.Nodes) > MaxOpenClashNodes {
		return fmt.Errorf("nodes: %d exceeds %d", len(r.Nodes), MaxOpenClashNodes)
	}
	seen := make(map[string]bool, len(r.Nodes))
	for i := range r.Nodes {
		node := &r.Nodes[i]
		if err := safeText("name", node.Name, 128); err != nil {
			return fmt.Errorf("nodes[%d]: %w", i, err)
		}
		if node.Name == "" {
			return fmt.Errorf("nodes[%d]: name must not be empty", i)
		}
		if seen[node.Name] {
			return fmt.Errorf("nodes[%d]: duplicate name %q", i, node.Name)
		}
		seen[node.Name] = true
		if node.DelayMS != nil && (*node.DelayMS < 0 || *node.DelayMS > 120000) {
			return fmt.Errorf("nodes[%d]: delay_ms must be 0..120000", i)
		}
		if node.Alive != nil && node.CheckedAt == nil {
			return fmt.Errorf("nodes[%d]: alive requires checked_at", i)
		}
		if node.DelayMS != nil && (node.CheckedAt == nil || node.Alive == nil || !*node.Alive) {
			return fmt.Errorf("nodes[%d]: delay_ms requires a live measured node", i)
		}
		if node.CheckedAt != nil && node.CheckedAt.IsZero() {
			return fmt.Errorf("nodes[%d]: checked_at must be set", i)
		}
		if node.ActiveConnections < 0 {
			return fmt.Errorf("nodes[%d]: active_connections must not be negative", i)
		}
	}
	if !r.Status.Valid() {
		return enumError("status", r.Status)
	}
	if r.ErrorClass != ErrNone && !r.ErrorClass.Valid() {
		return enumError("error_class", r.ErrorClass)
	}
	return safeText("message", r.Message, 160)
}
