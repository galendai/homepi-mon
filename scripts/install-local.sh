#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/common.sh
source "$script_dir/lib/common.sh"

install_dir="${HOMEPI_INSTALL_DIR:-$HOME/.local/bin}"
skip_build=false
dry_run=false
restart_service=true

usage() {
  printf '%s\n' 'usage: scripts/install-local.sh [--install-dir PATH] [--skip-build] [--no-restart] [--dry-run]'
}

while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --install-dir)
      [[ "$#" -ge 2 ]] || die "--install-dir requires a path"
      install_dir="$2"
      shift 2
      ;;
    --skip-build)
      skip_build=true
      shift
      ;;
    --no-restart)
      restart_service=false
      shift
      ;;
    --dry-run)
      dry_run=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      usage >&2
      die "unknown option: $1"
      ;;
  esac
done

case "$install_dir" in
  ''|/)
    die "refusing unsafe install directory: ${install_dir:-<empty>}"
    ;;
  /*) ;;
  *) die "install directory must be absolute: $install_dir" ;;
esac

source_dir="$repo_root/bin"
node_target="$install_dir/homepi-node"
service_was_running=false
service_was_unregistered=false
service_status="not installed"
host_os="$(uname -s)"

if [[ -x "$node_target" ]]; then
  if ! service_status="$($node_target status 2>&1)"; then
    die "could not determine existing homepi-node service state"
  fi
  if [[ "$service_status" == *"installed=true running=true"* ]]; then
    service_was_running=true
  fi
fi

log "install directory: $install_dir"
log "build before install: $([[ "$skip_build" == true ]] && printf no || printf yes)"
log "restart running service: $([[ "$restart_service" == true ]] && printf yes || printf no)"
log "existing service: ${service_status%%$'\n'*}"

if [[ "$dry_run" == true ]]; then
  log "dry-run complete; no files or services were changed"
  exit 0
fi

if [[ "$skip_build" == false ]]; then
  VERSION="$version" COMMIT="${COMMIT:-}" BUILD_DATE="${BUILD_DATE:-}" "$script_dir/build.sh"
fi

for binary in "${binaries[@]}"; do
  [[ -x "$source_dir/$binary" ]] || die "missing executable: $source_dir/$binary"
  "$source_dir/$binary" --version >/dev/null
done

mkdir -p "$install_dir"
stage_dir="$(mktemp -d "$install_dir/.homepi-install.XXXXXX")"

cleanup() {
  if [[ -n "${stage_dir:-}" && -d "$stage_dir" && "$stage_dir" == "$install_dir"/.homepi-install.* ]]; then
    rm -rf -- "$stage_dir"
  fi
}
trap cleanup EXIT HUP INT TERM

node_had_old=false
display_had_old=false
for binary in "${binaries[@]}"; do
  cp "$source_dir/$binary" "$stage_dir/$binary.new"
  chmod 0755 "$stage_dir/$binary.new"
  "$stage_dir/$binary.new" --version >/dev/null
  if [[ -e "$install_dir/$binary" ]]; then
    cp -p "$install_dir/$binary" "$stage_dir/$binary.old"
    if [[ "$binary" == homepi-node ]]; then
      node_had_old=true
    else
      display_had_old=true
    fi
  fi
done

install_targets() {
  local binary
  for binary in "${binaries[@]}"; do
    if [[ -f "$stage_dir/$binary.old" ]]; then
      cp -p "$stage_dir/$binary.old" "$stage_dir/$binary.previous"
      mv -f "$stage_dir/$binary.previous" "$install_dir/$binary.previous"
    fi
    mv -f "$stage_dir/$binary.new" "$install_dir/$binary"
  done
}

restore_targets() {
  local binary
  local had_old
  for binary in "${binaries[@]}"; do
    had_old=false
    if [[ "$binary" == homepi-node ]]; then
      had_old="$node_had_old"
    else
      had_old="$display_had_old"
    fi
    if [[ "$had_old" == true && -f "$stage_dir/$binary.old" ]]; then
      cp -p "$stage_dir/$binary.old" "$stage_dir/$binary.restore"
      mv -f "$stage_dir/$binary.restore" "$install_dir/$binary"
    elif [[ "$had_old" == false ]]; then
      rm -f -- "$install_dir/$binary"
    fi
  done
}

wait_for_running_service() {
  local attempt
  for attempt in {1..15}; do
    if "$node_target" status 2>&1 | grep -q 'installed=true running=true'; then
      return 0
    fi
    sleep 1
  done
  return 1
}

restore_running_service() {
  if [[ "$host_os" == Darwin && "$service_was_unregistered" == true ]]; then
    "$node_target" uninstall >/dev/null 2>&1 || true
    "$node_target" install >/dev/null 2>&1
    wait_for_running_service
    return
  fi
  "$node_target" start >/dev/null 2>&1
  wait_for_running_service
}

if [[ "$restart_service" == true && "$service_was_running" == true && "$host_os" == Darwin ]]; then
  log "unregister existing macOS LaunchAgent before replacing its executable"
  if ! "$node_target" uninstall; then
    die "could not unregister existing homepi-node LaunchAgent; binaries were not changed"
  fi
  service_was_unregistered=true
fi

if ! install_targets; then
  restore_targets
  if [[ "$service_was_unregistered" == true ]] && ! restore_running_service; then
    die "binary installation failed; previous targets were restored but service recovery failed"
  fi
  die "binary installation failed; previous targets were restored"
fi

if [[ "$restart_service" == true && "$service_was_running" == true ]]; then
  log "restart existing homepi-node user service"
  service_restart_ok=false
  if [[ "$host_os" == Darwin && "$service_was_unregistered" == true ]]; then
    if "$node_target" install && wait_for_running_service; then
      service_restart_ok=true
    fi
  elif "$node_target" stop && "$node_target" start && wait_for_running_service; then
    service_restart_ok=true
  fi
  if [[ "$service_restart_ok" != true ]]; then
    log "new service failed; restore previous binaries"
    if [[ "$service_was_unregistered" == true ]]; then
      "$node_target" uninstall >/dev/null 2>&1 || true
    fi
    restore_targets
    if ! restore_running_service; then
      die "service restart failed; previous binaries were restored but service recovery failed"
    fi
    die "service restart failed; previous binaries and running service were restored"
  fi
fi

for binary in "${binaries[@]}"; do
  "$install_dir/$binary" --version
done

log "local installation completed"
