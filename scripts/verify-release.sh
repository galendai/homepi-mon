#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/common.sh
source "$script_dir/lib/common.sh"

require_command shasum

skip_build=false
case "${1:-}" in
  '') ;;
  --skip-build) skip_build=true ;;
  *) die "usage: scripts/verify-release.sh [--skip-build]" ;;
esac

if [[ "$skip_build" == false ]]; then
  VERSION="$version" COMMIT="${COMMIT:-}" BUILD_DATE="${BUILD_DATE:-}" "$script_dir/dist.sh"
fi

dist_dir="$repo_root/dist"
[[ -d "$dist_dir" ]] || die "dist directory not found; run scripts/dist.sh first"

expected=()
for platform in "${platforms[@]}"; do
  for binary in "${binaries[@]}"; do
    artifact="$(artifact_name "$binary" "$platform")"
    [[ -f "$dist_dir/$artifact" ]] || die "missing release artifact: $artifact"
    expected+=("$artifact")
  done
done

[[ "${#expected[@]}" -eq 12 ]] || die "expected 12 release artifacts"

actual_count="$(find "$dist_dir" -maxdepth 1 -type f \
  \( -name "homepi-node-$version-*" -o -name "homepi-display-$version-*" \) \
  -print | wc -l | tr -d ' ')"
[[ "$actual_count" == 12 ]] || \
  die "expected exactly 12 current-version artifacts, found $actual_count"

checksum_tmp="$(mktemp "$dist_dir/.SHA256SUMS.XXXXXX")"
cleanup() {
  if [[ -n "${checksum_tmp:-}" && -f "$checksum_tmp" && "$checksum_tmp" == "$dist_dir"/.SHA256SUMS.* ]]; then
    rm -f -- "$checksum_tmp"
  fi
}
trap cleanup EXIT HUP INT TERM

(
  cd "$dist_dir"
  shasum -a 256 "${expected[@]}"
) >"$checksum_tmp"

line_count="$(wc -l <"$checksum_tmp" | tr -d ' ')"
[[ "$line_count" == 12 ]] || die "expected 12 checksum entries, found $line_count"

mv -f "$checksum_tmp" "$dist_dir/SHA256SUMS"
checksum_tmp=""

(
  cd "$dist_dir"
  shasum -a 256 -c SHA256SUMS
)

log "release verification passed with 12 artifacts"
