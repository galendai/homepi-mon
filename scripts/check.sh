#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/common.sh
source "$script_dir/lib/common.sh"

require_command go
require_command gofmt

cd "$repo_root"

log "gofmt"
unformatted="$(gofmt -l cmd internal)"
if [[ -n "$unformatted" ]]; then
  printf '%s\n' "$unformatted"
  die "run: make fmt"
fi

log "go vet"
go vet ./...

log "go test"
go test ./...
