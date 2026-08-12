package protocol

import (
	"errors"
	"fmt"
	"time"
)

const (
	MaxHomeLabNodes    = 100
	MaxHomeLabServices = 32
)

// HomeLabNode is one bounded current node summary. It deliberately contains
// no sample history or arbitrary labels from Prometheus.
type HomeLabNode struct {
	ID                 string       `json:"id"`
	Name               string       `json:"name"`
	Online             *bool        `json:"online,omitempty"`
	CPUPercent         *int         `json:"cpu_percent,omitempty"`
	MemoryPercent      *int         `json:"memory_percent,omitempty"`
	DiskPercent        *int         `json:"disk_percent,omitempty"`
	NetworkReceiveBPS  *int64       `json:"network_receive_bps,omitempty"`
	NetworkTransmitBPS *int64       `json:"network_transmit_bps,omitempty"`
	ObservedAt         time.Time    `json:"observed_at"`
	StaleAfter         Duration     `json:"stale_after,omitempty"`
	Status             MetricStatus `json:"status"`
	ErrorClass         ErrorClass   `json:"error_class,omitempty"`
	Message            string       `json:"message,omitempty"`
	Order              int          `json:"order,omitempty"`
}

// HomeLabService is one bounded current service summary. Optional pointer
// counts distinguish an unavailable capability from a real zero.
type HomeLabService struct {
	ID                 string       `json:"id"`
	Name               string       `json:"name"`
	Kind               string       `json:"kind"`
	Version            string       `json:"version,omitempty"`
	APIGeneration      string       `json:"api_generation,omitempty"`
	Healthy            *bool        `json:"healthy,omitempty"`
	FiringAlerts       *int         `json:"firing_alerts,omitempty"`
	EnvironmentsTotal  *int         `json:"environments_total,omitempty"`
	EnvironmentsOnline *int         `json:"environments_online,omitempty"`
	ContainersRunning  *int         `json:"containers_running,omitempty"`
	ContainersStopped  *int         `json:"containers_stopped,omitempty"`
	ContainersFailed   *int         `json:"containers_failed,omitempty"`
	Stacks             *int         `json:"stacks,omitempty"`
	ObservedAt         time.Time    `json:"observed_at"`
	StaleAfter         Duration     `json:"stale_after,omitempty"`
	Status             MetricStatus `json:"status"`
	ErrorClass         ErrorClass   `json:"error_class,omitempty"`
	Message            string       `json:"message,omitempty"`
	Order              int          `json:"order,omitempty"`
}

// HomeLabReport is the current bounded output of one HomeLab connector.
type HomeLabReport struct {
	Nodes    []HomeLabNode
	Services []HomeLabService
}

func (n *HomeLabNode) Validate() error {
	if err := validID("id", n.ID); err != nil {
		return err
	}
	if err := safeText("name", n.Name, 64); err != nil {
		return err
	}
	if n.Name == "" {
		return errors.New("name: must not be empty")
	}
	for field, value := range map[string]*int{
		"cpu_percent":    n.CPUPercent,
		"memory_percent": n.MemoryPercent,
		"disk_percent":   n.DiskPercent,
	} {
		if value != nil && (*value < 0 || *value > 100) {
			return fmt.Errorf("%s: must be 0..100", field)
		}
	}
	if n.NetworkReceiveBPS != nil && *n.NetworkReceiveBPS < 0 {
		return errors.New("network_receive_bps: must not be negative")
	}
	if n.NetworkTransmitBPS != nil && *n.NetworkTransmitBPS < 0 {
		return errors.New("network_transmit_bps: must not be negative")
	}
	if n.ObservedAt.IsZero() {
		return errors.New("observed_at: must be set")
	}
	if n.StaleAfter.D() < 0 {
		return errors.New("stale_after: must not be negative")
	}
	if !n.Status.Valid() {
		return enumError("status", n.Status)
	}
	if n.ErrorClass != ErrNone && !n.ErrorClass.Valid() {
		return enumError("error_class", n.ErrorClass)
	}
	return safeText("message", n.Message, 160)
}

func (s *HomeLabService) Validate() error {
	if err := validID("id", s.ID); err != nil {
		return err
	}
	if err := safeText("name", s.Name, 64); err != nil {
		return err
	}
	if s.Name == "" {
		return errors.New("name: must not be empty")
	}
	if err := validID("kind", s.Kind); err != nil {
		return err
	}
	if err := safeText("version", s.Version, 64); err != nil {
		return err
	}
	if err := safeText("api_generation", s.APIGeneration, 32); err != nil {
		return err
	}
	counts := map[string]*int{
		"firing_alerts":       s.FiringAlerts,
		"environments_total":  s.EnvironmentsTotal,
		"environments_online": s.EnvironmentsOnline,
		"containers_running":  s.ContainersRunning,
		"containers_stopped":  s.ContainersStopped,
		"containers_failed":   s.ContainersFailed,
		"stacks":              s.Stacks,
	}
	for field, value := range counts {
		if value != nil && *value < 0 {
			return fmt.Errorf("%s: must not be negative", field)
		}
	}
	if s.EnvironmentsOnline != nil && s.EnvironmentsTotal != nil &&
		*s.EnvironmentsOnline > *s.EnvironmentsTotal {
		return errors.New("environments_online: exceeds environments_total")
	}
	if s.ObservedAt.IsZero() {
		return errors.New("observed_at: must be set")
	}
	if s.StaleAfter.D() < 0 {
		return errors.New("stale_after: must not be negative")
	}
	if !s.Status.Valid() {
		return enumError("status", s.Status)
	}
	if s.ErrorClass != ErrNone && !s.ErrorClass.Valid() {
		return enumError("error_class", s.ErrorClass)
	}
	return safeText("message", s.Message, 160)
}

func (n HomeLabNode) clone() HomeLabNode {
	out := n
	out.Online = cloneBool(n.Online)
	out.CPUPercent = cloneInt(n.CPUPercent)
	out.MemoryPercent = cloneInt(n.MemoryPercent)
	out.DiskPercent = cloneInt(n.DiskPercent)
	out.NetworkReceiveBPS = cloneInt64(n.NetworkReceiveBPS)
	out.NetworkTransmitBPS = cloneInt64(n.NetworkTransmitBPS)
	return out
}

func (s HomeLabService) clone() HomeLabService {
	out := s
	out.Healthy = cloneBool(s.Healthy)
	out.FiringAlerts = cloneInt(s.FiringAlerts)
	out.EnvironmentsTotal = cloneInt(s.EnvironmentsTotal)
	out.EnvironmentsOnline = cloneInt(s.EnvironmentsOnline)
	out.ContainersRunning = cloneInt(s.ContainersRunning)
	out.ContainersStopped = cloneInt(s.ContainersStopped)
	out.ContainersFailed = cloneInt(s.ContainersFailed)
	out.Stacks = cloneInt(s.Stacks)
	return out
}

// Clone returns a deep copy so connectors and current state do not share
// mutable pointer fields.
func (r HomeLabReport) Clone() HomeLabReport {
	out := HomeLabReport{
		Nodes:    make([]HomeLabNode, len(r.Nodes)),
		Services: make([]HomeLabService, len(r.Services)),
	}
	for i := range r.Nodes {
		out.Nodes[i] = r.Nodes[i].clone()
	}
	for i := range r.Services {
		out.Services[i] = r.Services[i].clone()
	}
	return out
}

func cloneBool(v *bool) *bool {
	if v == nil {
		return nil
	}
	out := *v
	return &out
}

func cloneInt(v *int) *int {
	if v == nil {
		return nil
	}
	out := *v
	return &out
}

func cloneInt64(v *int64) *int64 {
	if v == nil {
		return nil
	}
	out := *v
	return &out
}
