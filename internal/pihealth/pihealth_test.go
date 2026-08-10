package pihealth

import (
	"runtime"
	"testing"
)

// U-H001: on a host without Linux sysfs entries the reader reports "unknown"
// instead of zeros the UI would render as real measurements.
func TestReadDegradesGracefully(t *testing.T) {
	r := NewReader()
	got := r.Read()

	if runtime.GOOS != "linux" {
		if got.Known {
			t.Errorf("non-Linux host reported known health: %+v", got)
		}
		return
	}
	// On Linux the second sample is the one that can report CPU.
	got = r.Read()
	if !got.Known {
		t.Fatal("Linux host reported no health at all")
	}
	if got.CPUPercent < 0 || got.CPUPercent > 100 {
		t.Errorf("cpu = %d, out of range", got.CPUPercent)
	}
	if got.MemPercent < 0 || got.MemPercent > 100 {
		t.Errorf("mem = %d, out of range", got.MemPercent)
	}
}

// U-H002: percentages are always clamped into range.
func TestClampPercent(t *testing.T) {
	cases := map[int]int{-5: 0, 0: 0, 50: 50, 100: 100, 250: 100}
	for in, want := range cases {
		if got := clampPercent(in); got != want {
			t.Errorf("clampPercent(%d) = %d, want %d", in, got, want)
		}
	}
}
