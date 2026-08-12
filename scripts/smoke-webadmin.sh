#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/common.sh
source "$script_dir/lib/common.sh"

manual_root="$(mktemp -d)"
manual_bin="$manual_root/homepi-node"
manual_config="$manual_root/config.json"
manual_data="$manual_root/data"
manual_secrets="$manual_root/secrets"
webadmin_addr="${HOMEPI_WEBADMIN_ADDR:-127.0.0.1:18765}"

cleanup() {
  if [[ -d "$manual_root" ]]; then
    case "$manual_root" in
      /tmp/*|/private/tmp/*|/var/folders/*) rm -rf -- "$manual_root" ;;
      *) printf 'warning: refusing to clean unexpected path: %s\n' "$manual_root" >&2 ;;
    esac
  fi
}
trap cleanup EXIT HUP INT TERM

mkdir -p "$manual_data" "$manual_secrets"
chmod 0700 "$manual_root" "$manual_data" "$manual_secrets"

cd "$repo_root"
go build -o "$manual_bin" ./cmd/homepi-node

export HOMEPI_DATA_DIR="$manual_data"
export HOMEPI_SECRET_DIR="$manual_secrets"
unset HOMEPI_PROVIDER_SECRET HOMEPI_NODE_CONFIG

"$manual_bin" config init -out "$manual_config"
"$manual_bin" config validate -config "$manual_config"

printf 'isolated config: %s\n' "$manual_config"
printf 'warning: do not click Apply; the Web Admin uses the real user service manager\n'
"$manual_bin" configure -config "$manual_config" -addr "$webadmin_addr" -no-browser "$@"
