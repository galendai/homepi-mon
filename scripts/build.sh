#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/common.sh
source "$script_dir/lib/common.sh"

require_command go

bin_dir="$repo_root/bin"
stage_dir=""

cleanup() {
  if [[ -n "$stage_dir" && -d "$stage_dir" && "$stage_dir" == "$bin_dir"/.build.* ]]; then
    rm -rf -- "$stage_dir"
  fi
}
trap cleanup EXIT HUP INT TERM

mkdir -p "$bin_dir"
stage_dir="$(mktemp -d "$bin_dir/.build.XXXXXX")"

commit="$(resolve_commit)"
build_date="$(resolve_build_date)"
ldflags="$(build_ldflags "$commit" "$build_date")"

cd "$repo_root"
for binary in "${binaries[@]}"; do
  log "build $binary for the current platform"
  go build -trimpath -ldflags "$ldflags" -o "$stage_dir/$binary" "./cmd/$binary"
  chmod 0755 "$stage_dir/$binary"
  "$stage_dir/$binary" --version >/dev/null
done

for binary in "${binaries[@]}"; do
  mv -f "$stage_dir/$binary" "$bin_dir/$binary"
done

log "local binaries are ready in $bin_dir"
