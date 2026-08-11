// Package kioskunit contains the audited DietPi systemd deployment artifacts.
package kioskunit

// SystemdUnit binds the output-only kiosk to tty1. All runtime values,
// including the device token, come from a root-readable environment file and
// never appear in the unit or process arguments.
const SystemdUnit = `[Unit]
Description=HomePi Monitor display kiosk
Wants=network-online.target
After=network-online.target
Conflicts=getty@tty1.service
After=getty@tty1.service
StartLimitIntervalSec=300
StartLimitBurst=5

[Service]
Type=simple
User=homepi-display
Group=homepi-display
EnvironmentFile=/etc/homepi-display/environment
ExecStart=/usr/local/bin/homepi-display run
WorkingDirectory=/var/lib/homepi-display
StandardInput=null
StandardOutput=tty
StandardError=journal
TTYPath=/dev/tty1
TTYReset=yes
TTYVHangup=yes
TTYVTDisallocate=yes
Restart=on-failure
RestartSec=10s
NoNewPrivileges=yes
PrivateTmp=yes
ProtectHome=yes
ProtectSystem=strict
ReadWritePaths=/var/lib/homepi-display

[Install]
WantedBy=multi-user.target
`

// EnvironmentExample is copied to /etc/homepi-display/environment and must be
// chmod 0600 before the service starts.
const EnvironmentExample = `# chmod 0600 /etc/homepi-display/environment
HOMEPI_NODE_URL=https://dev-mac.lan:8443
HOMEPI_DEVICE_ID=pi-kiosk
HOMEPI_SOURCE_NODE_ID=dev-mac
HOMEPI_NODE_CERT_PIN=sha256:REPLACE_WITH_HOMEPI_NODE_DOCTOR_FINGERPRINT
HOMEPI_DEVICE_TOKEN=REPLACE_WITH_DEVICE_ADD_TOKEN
HOMEPI_DISPLAY_DATA_DIR=/var/lib/homepi-display
HOMEPI_DISPLAY_STYLE=rich
`
