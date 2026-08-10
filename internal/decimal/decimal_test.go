package decimal

import (
	"encoding/json"
	"testing"
)

func TestParseRoundTripPreservesScale(t *testing.T) {
	for _, s := range []string{"0", "8.20", "-1234.5", "49.58", "0.000000001", "100"} {
		d, err := Parse(s)
		if err != nil {
			t.Fatalf("Parse(%q) error: %v", s, err)
		}
		if got := d.String(); got != s {
			t.Errorf("Parse(%q).String() = %q, want %q", s, got, s)
		}
	}
}

func TestParseRejectsBadInput(t *testing.T) {
	for _, s := range []string{"", " ", ".", "1.", "1.2.3", "abc", "1e5", "0.0000000001", "+"} {
		if _, err := Parse(s); err == nil {
			t.Errorf("Parse(%q) succeeded, want error", s)
		}
	}
}

func TestCmpAcrossScales(t *testing.T) {
	a := MustParse("8.20")
	b := MustParse("8.2")
	if a.Cmp(b) != 0 {
		t.Errorf("8.20 vs 8.2 = %d, want 0", a.Cmp(b))
	}
	if MustParse("8.21").Cmp(a) != 1 {
		t.Error("8.21 should be greater than 8.20")
	}
	if MustParse("-1").Cmp(a) != -1 {
		t.Error("-1 should be less than 8.20")
	}
}

func TestJSONRoundTripIsExact(t *testing.T) {
	type wrapper struct {
		V Decimal `json:"v"`
	}
	// A JSON number that binary float64 cannot represent exactly.
	raw := []byte(`{"v":0.1}`)
	var w wrapper
	if err := json.Unmarshal(raw, &w); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if w.V.String() != "0.1" {
		t.Errorf("got %q, want 0.1", w.V.String())
	}
	out, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(out) != `{"v":"0.1"}` {
		t.Errorf("marshal = %s, want {\"v\":\"0.1\"}", out)
	}
}

func TestPercentOf(t *testing.T) {
	cases := []struct {
		value, limit string
		want         int
		ok           bool
	}{
		{"68", "100", 68, true},
		{"1", "3", 33, true},
		{"2", "3", 67, true},
		{"32", "100", 32, true},
		{"5", "0", 0, false},
		{"5", "-1", 0, false},
	}
	for _, c := range cases {
		got, ok := PercentOf(MustParse(c.value), MustParse(c.limit))
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("PercentOf(%s,%s) = %d,%v want %d,%v", c.value, c.limit, got, ok, c.want, c.ok)
		}
	}
}

func TestRescaleDisplayOnly(t *testing.T) {
	if got := MustParse("8.205").Rescale(2); got != "8.21" {
		t.Errorf("Rescale = %q, want 8.21", got)
	}
	if got := MustParse("8").Rescale(2); got != "8.00" {
		t.Errorf("Rescale = %q, want 8.00", got)
	}
}
