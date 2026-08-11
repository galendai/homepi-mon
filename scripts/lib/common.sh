#!/usr/bin/env bash

set -euo pipefail

common_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd -- "$common_dir/../.." && pwd)"

readonly common_dir repo_root
readonly module_path="github.com/galendai/homepi-mon"
readonly version="${VERSION:-0.1.0}"

binaries=(homepi-node homepi-display)
platforms=(
  darwin/amd64
  darwin/arm64
  windows/amd64
  linux/amd64
  linux/arm64
  linux/arm/v7
)

die() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

log() {
  printf '==> %s\n' "$*"
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

validate_version() {
  case "$version" in
    ''|*[!0-9A-Za-z._+-]*)
      die "VERSION contains unsupported characters: $version"
      ;;
  esac
}

resolve_commit() {
  if [[ -n "${COMMIT:-}" ]]; then
    printf '%s\n' "$COMMIT"
    return
  fi
  git -C "$repo_root" rev-parse --short HEAD 2>/dev/null || printf 'unknown\n'
}

resolve_build_date() {
  if [[ -n "${BUILD_DATE:-}" ]]; then
    printf '%s\n' "$BUILD_DATE"
    return
  fi
  date -u +%Y-%m-%dT%H:%M:%SZ
}

build_ldflags() {
  local commit="$1"
  local build_date="$2"
  printf '%s\n' "-s -w -X $module_path/internal/buildinfo.Version=$version -X $module_path/internal/buildinfo.Commit=$commit -X $module_path/internal/buildinfo.Date=$build_date"
}

artifact_name() {
  local binary="$1"
  local platform="$2"
  local goos="${platform%%/*}"
  local rest="${platform#*/}"
  local goarch="${rest%%/*}"
  local suffix=""
  local tag="$goos-$goarch"

  if [[ "$rest" == */* ]]; then
    local goarm="${rest##*/}"
    goarm="${goarm#v}"
    tag="${tag}v${goarm}"
  fi
  if [[ "$goos" == windows ]]; then
    suffix=".exe"
  fi
  printf '%s\n' "$binary-$version-$tag$suffix"
}

validate_version
