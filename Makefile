# HomePi Monitor build entry.
#
# Version metadata is injected at link time so `--version` reports the real
# commit and build date instead of the in-source development defaults.

VERSION    ?= 0.1.0

BIN_DIR    := bin
DIST_DIR   := dist
SCRIPTS_DIR := scripts

.PHONY: all build test vet fmt check clean dist checksums verify-release install-local golden run-node run-display

all: check build

build:
	@VERSION="$(VERSION)" $(SCRIPTS_DIR)/build.sh

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w cmd internal

# check is the gate every task must pass before it can be marked DONE.
check:
	@$(SCRIPTS_DIR)/check.sh

# golden regenerates the 60x20 reference screens. Review the diff by eye:
# these files are the visual contract with UI-Spec-001.
golden:
	UPDATE_GOLDEN=1 go test ./internal/ui/
	@echo "regenerated internal/ui/testdata/*.txt"

dist:
	@VERSION="$(VERSION)" $(SCRIPTS_DIR)/dist.sh

checksums:
	@VERSION="$(VERSION)" $(SCRIPTS_DIR)/verify-release.sh

verify-release: checksums

install-local:
	@VERSION="$(VERSION)" $(SCRIPTS_DIR)/install-local.sh

clean:
	rm -rf $(BIN_DIR) $(DIST_DIR)

# run-node starts the Phase 1 mock daemon against the example fixture.
run-node: build
	$(BIN_DIR)/homepi-node serve \
	  -node-id dev-mac -node-label DEV-MAC \
	  -device-id pi-kiosk -device-token $${HOMEPI_DEVICE_TOKEN:-dev-token-0123456789abcdef} \
	  -mock-fixture examples/mock-fixture.json \
	  -addr 127.0.0.1:8443 -interval 5s -no-tls

# run-display attaches a local kiosk to the daemon started by run-node.
run-display: build
	HOMEPI_DEVICE_TOKEN=$${HOMEPI_DEVICE_TOKEN:-dev-token-0123456789abcdef} \
	  $(BIN_DIR)/homepi-display run \
	  -node-url http://127.0.0.1:8443 -no-tls \
	  -device-id pi-kiosk -node-id dev-mac \
	  -data-dir ./tmp/display-data
