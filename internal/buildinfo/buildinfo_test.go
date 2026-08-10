package buildinfo

import (
	"strings"
	"testing"
)

func TestUIVersionIsASCIIAndBounded(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"0.1.0", "V0.1.0"},
		{"0.1.0-dev", "V0.1.0"},
		{"1.2.3+build.9", "V1.2.3"},
		{"12.345.6789", "V12.345.67"},
	}
	orig := Version
	defer func() { Version = orig }()

	for _, c := range cases {
		Version = c.in
		got := UIVersion()
		if got != c.want {
			t.Errorf("UIVersion(%q) = %q, want %q", c.in, got, c.want)
		}
		if len(got) > 10 {
			t.Errorf("UIVersion(%q) = %q exceeds 10 cells", c.in, got)
		}
		for _, r := range got {
			if r < 0x20 || r > 0x7e {
				t.Errorf("UIVersion(%q) = %q contains non-printable-ASCII %q", c.in, got, r)
			}
		}
	}
}

func TestStringContainsPlatform(t *testing.T) {
	out := String("homepi-node")
	for _, want := range []string{"homepi-node", "commit:", "built:", "platform:", "go:"} {
		if !strings.Contains(out, want) {
			t.Errorf("String() missing %q:\n%s", want, out)
		}
	}
}
