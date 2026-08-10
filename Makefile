# HomePi Monitor build entry.
#
# Version metadata is injected at link time so `--version` reports the real
# commit and build date instead of the in-source development defaults.

BINARIES   := homepi-node homepi-display
MODULE     := github.com/galendai/homepi-mon
VERSION    ?= 0.1.0
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE       := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS    := -s -w \
  -X $(MODULE)/internal/buildinfo.Version=$(VERSION) \
  -X $(MODULE)/internal/buildinfo.Commit=$(COMMIT) \
  -X $(MODULE)/internal/buildinfo.Date=$(DATE)

BIN_DIR    := bin
DIST_DIR   := dist

# Phase 1 target matrix (Development-Plan P1-01). linux/arm/v7 is the
# Raspberry Pi 3 B+ kiosk target; the rest are homepi-node hosts.
PLATFORMS := \
  darwin/amd64 \
  darwin/arm64 \
  windows/amd64 \
  linux/amd64 \
  linux/arm64 \
  linux/arm/v7

.PHONY: all build test vet fmt check clean dist checksums golden run-node run-display

all: check build

build: $(BIN_DIR)
	@for b in $(BINARIES); do \
	  echo "build $$b"; \
	  go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$$b ./cmd/$$b || exit 1; \
	done

$(BIN_DIR):
	@mkdir -p $@

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w cmd internal

# check is the gate every task must pass before it can be marked DONE.
check:
	@echo "== gofmt =="
	@test -z "$$(gofmt -l cmd internal)" || { gofmt -l cmd internal; echo "run: make fmt"; exit 1; }
	@echo "== go vet =="
	@go vet ./...
	@echo "== go test =="
	@go test ./...

# golden regenerates the 60x20 reference screens. Review the diff by eye:
# these files are the visual contract with UI-Spec-001.
golden:
	UPDATE_GOLDEN=1 go test ./internal/ui/
	@echo "regenerated internal/ui/testdata/*.txt"

# dist cross-compiles the full Phase 1 matrix.
dist:
	@mkdir -p $(DIST_DIR)
	@for p in $(PLATFORMS); do \
	  goos=$${p%%/*}; rest=$${p#*/}; goarch=$${rest%%/*}; goarm=""; \
	  case "$$rest" in */v7) goarm=7;; esac; \
	  suffix=""; [ "$$goos" = windows ] && suffix=".exe"; \
	  tag="$$goos-$$goarch$${goarm:+v$$goarm}"; \
	  for b in $(BINARIES); do \
	    echo "dist $$b $$tag"; \
	    GOOS=$$goos GOARCH=$$goarch GOARM=$$goarm CGO_ENABLED=0 \
	      go build -trimpath -ldflags "$(LDFLAGS)" \
	      -o $(DIST_DIR)/$$b-$(VERSION)-$$tag$$suffix ./cmd/$$b || exit 1; \
	  done; \
	done

checksums: dist
	@cd $(DIST_DIR) && shasum -a 256 * > SHA256SUMS && cat SHA256SUMS

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
