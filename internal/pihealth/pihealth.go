// Package pihealth reports the display device's own status for the footer
// device line.
//
// Everything here is read-only and best effort: on a host without the
// Raspberry Pi's sysfs entries the values are reported as unknown, which the
// renderer prints as "--" rather than as a misleading zero.
package pihealth

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Reading is one sample of local device health.
type Reading struct {
	TempC      int
	CPUPercent int
	MemPercent int
	// Known is false when this platform exposes none of the values.
	Known bool
}

// Reader samples local health. CPU percentage needs two samples, so the reader
// keeps the previous /proc/stat totals between calls.
type Reader struct {
	mu       sync.Mutex
	prevIdle uint64
	prevAll  uint64
	haveCPU  bool
}

// NewReader builds a Reader.
func NewReader() *Reader { return &Reader{} }

// Read samples temperature, CPU and memory.
func (r *Reader) Read() Reading {
	out := Reading{}
	if t, ok := readTempC(); ok {
		out.TempC = t
		out.Known = true
	}
	if c, ok := r.readCPUPercent(); ok {
		out.CPUPercent = c
		out.Known = true
	}
	if m, ok := readMemPercent(); ok {
		out.MemPercent = m
		out.Known = true
	}
	return out
}

// SampleInterval is the recommended gap between Read calls. The CPU figure is
// an average over the interval, and MOD-002 6 caps the clock refresh at 30
// seconds, so sampling faster buys nothing.
const SampleInterval = 30 * time.Second

func readTempC() (int, bool) {
	raw, err := os.ReadFile("/sys/class/thermal/thermal_zone0/temp")
	if err != nil {
		return 0, false
	}
	milli, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return 0, false
	}
	return milli / 1000, true
}

// readCPUPercent returns busy CPU percentage since the previous call.
func (r *Reader) readCPUPercent() (int, bool) {
	raw, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, false
	}
	line, _, _ := strings.Cut(string(raw), "\n")
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, false
	}
	var all, idle uint64
	for i, f := range fields[1:] {
		v, err := strconv.ParseUint(f, 10, 64)
		if err != nil {
			return 0, false
		}
		all += v
		// Fields 4 and 5 of the cpu line are idle and iowait.
		if i == 3 || i == 4 {
			idle += v
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.haveCPU {
		r.prevAll, r.prevIdle, r.haveCPU = all, idle, true
		// The first sample has no interval to average over.
		return 0, false
	}
	deltaAll := all - r.prevAll
	deltaIdle := idle - r.prevIdle
	r.prevAll, r.prevIdle = all, idle
	if deltaAll == 0 {
		return 0, false
	}
	busy := float64(deltaAll-deltaIdle) / float64(deltaAll) * 100
	return clampPercent(int(busy + 0.5)), true
}

func readMemPercent() (int, bool) {
	raw, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, false
	}
	var total, available uint64
	for _, line := range strings.Split(string(raw), "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		fields := strings.Fields(value)
		if len(fields) == 0 {
			continue
		}
		v, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		switch key {
		case "MemTotal":
			total = v
		case "MemAvailable":
			available = v
		}
	}
	if total == 0 || available > total {
		return 0, false
	}
	used := float64(total-available) / float64(total) * 100
	return clampPercent(int(used + 0.5)), true
}

func clampPercent(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}
