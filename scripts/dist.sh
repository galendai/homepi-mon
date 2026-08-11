#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/common.sh
source "$script_dir/lib/common.sh"

require_command go

dist_dir="$repo_root/dist"
stage_dir=""

cleanup() {
  if [[ -n "$stage_dir" && -d "$stage_dir" && "$stage_dir" == "$dist_dir"/.build.* ]]; then
    rm -rf -- "$stage_dir"
  fi
}
trap cleanup EXIT HUP INT TERM

mkdir -p "$dist_dir"
stage_dir="$(mktemp -d "$dist_dir/.build.XXXXXX")"

commit="$(resolve_commit)"
build_date="$(resolve_build_date)"
ldflags="$(build_ldflags "$commit" "$build_date")"

cd "$repo_root"
for platform in "${platforms[@]}"; do
  goos="${platform%%/*}"
  rest="${platform#*/}"
  goarch="${rest%%/*}"
  goarm=""
  if [[ "$rest" == */* ]]; then
    goarm="${rest##*/}"
    goarm="${goarm#v}"
  fi

  for binary in "${binaries[@]}"; do
    output="$(artifact_name "$binary" "$platform")"
    log "dist $binary $platform"
    if [[ -n "$goarm" ]]; then
      GOOS="$goos" GOARCH="$goarch" GOARM="$goarm" CGO_ENABLED=0 \
        go build -trimpath -ldflags "$ldflags" -o "$stage_dir/$output" "./cmd/$binary"
    else
      GOOS="$goos" GOARCH="$goarch" CGO_ENABLED=0 \
        go build -trimpath -ldflags "$ldflags" -o "$stage_dir/$output" "./cmd/$binary"
    fi
    chmod 0755 "$stage_dir/$output"
  done
done

for platform in "${platforms[@]}"; do
  for binary in "${binaries[@]}"; do
    output="$(artifact_name "$binary" "$platform")"
    mv -f "$stage_dir/$output" "$dist_dir/$output"
  done
done

log "release binaries are ready in $dist_dir"
