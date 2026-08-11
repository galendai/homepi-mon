package kioskunit

import (
	"strings"
	"testing"
)

func TestDietPiUnitSecurityAndRecoveryContract(t *testing.T) {
	required := []string{
		"EnvironmentFile=/etc/homepi-display/environment",
		"ExecStart=/usr/local/bin/homepi-display run",
		"Conflicts=getty@tty1.service", "After=getty@tty1.service",
		"StandardInput=null", "TTYPath=/dev/tty1", "Restart=on-failure",
		"RestartSec=10s", "StartLimitBurst=5", "WantedBy=multi-user.target",
	}
	for _, item := range required {
		if !strings.Contains(SystemdUnit, item) {
			t.Errorf("unit missing %q", item)
		}
	}
	for _, forbidden := range []string{"HOMEPI_DEVICE_TOKEN=", "--token", "-token"} {
		if strings.Contains(SystemdUnit, forbidden) {
			t.Errorf("unit contains forbidden token material %q", forbidden)
		}
	}
	if !strings.Contains(EnvironmentExample, "chmod 0600") ||
		!strings.Contains(EnvironmentExample, "HOMEPI_DEVICE_TOKEN=REPLACE_") ||
		!strings.Contains(EnvironmentExample, "HOMEPI_DISPLAY_STYLE=rich") {
		t.Fatal("environment template does not document secure token injection")
	}
}
