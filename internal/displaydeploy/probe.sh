bash -s <<'HOMEPI_READONLY_PROBE'
set -eu
export LC_ALL=C
unit=homepi-display.service
file=/etc/homepi-display/environment
exec 8</etc/homepi-display
flock -s -w 10 8
test ! -L "$file"
test -f "$file"
test "$(stat -c %U:%G:%a "$file")" = root:root:600
printf 'file_digest=%s\n' "$(sha256sum "$file" | cut -d' ' -f1)"

# Validate on the Pi before emitting any argv or environment value. Unknown
# text stays on the device, even if it was accidentally supplied as a flag.
emit_setting() {
  local scope=$1 key=$2 value=$3 pattern page='(HOME|CODING|API|HOMELAB|SERVICES|SYSTEM|OPENCLASH)'
  local space='[[:blank:]]*'
  case "$key" in
    style) pattern='^(RICH|ASCII)$' ;;
    home-only) pattern='^(TRUE|FALSE|T|F|1|0)$' ;;
    page-only) pattern="^$space$page$space$" ;;
    pages|page-order) pattern="^$space$page$space(,$space$page$space)*$" ;;
    page-dwell-seconds) pattern="^$space$page$space:$space[0-9]{1,3}$space(,$space$page$space:$space[0-9]{1,3}$space)*$" ;;
    *) return ;;
  esac
  if [[ -n "$value" && ( ${#value} -gt 256 || ! "${value^^}" =~ $pattern ) ]]; then
    value='!invalid'
  fi
  printf '%s%s=%s\n' "$scope" "$key" "$value"
}
read_setting() {
  local scope=$1 entry=$2
  case "${entry%%=*}" in
    HOMEPI_DISPLAY_STYLE) emit_setting "$scope" style "${entry#*=}" ;;
    HOMEPI_HOME_ONLY) emit_setting "$scope" home-only "${entry#*=}" ;;
    HOMEPI_PAGE_ORDER) emit_setting "$scope" page-order "${entry#*=}" ;;
    HOMEPI_PAGE_DWELL_SECONDS) emit_setting "$scope" page-dwell-seconds "${entry#*=}" ;;
    HOMEPI_ROTATION_PAGES) emit_setting "$scope" pages "${entry#*=}" ;;
    HOMEPI_PAGE_LOCK) emit_setting "$scope" page-only "${entry#*=}" ;;
  esac
}
while IFS= read -r line || [[ -n "$line" ]]; do
  read_setting file_ "$line"
  case "$line" in
    HOMEPI_DEVICE_ID=*) id=${line#*=}; [[ "$id" =~ ^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$ ]] && printf 'device_id=%s\n' "$id" ;;
    HOMEPI_NODE_URL=*) printf 'node_url_hash=%s\n' "$(printf %s "${line#*=}" | sha256sum | cut -d' ' -f1)" ;;
  esac
done < "$file"
flock -u 8
printf 'active=%s\n' "$(systemctl is-active "$unit" 2>/dev/null || true)"
printf 'restarts=%s\n' "$(systemctl show "$unit" -p NRestarts --value)"
printf 'binary=%s\n' "$(homepi-display version | head -n1 | tr ' ' '_')"
capability=$(homepi-display config capabilities 2>/dev/null || true)
case "$capability" in
  'rotation-env-v1 gui'|'rotation-env-v1 terminal') printf 'rotation_env=yes\n' ;;
esac
paths=$(systemctl show "$unit" -p DropInPaths --value)
for path in $paths; do
  name=${path##*/}
  if [[ "$name" =~ ^[a-zA-Z0-9_.-]{1,100}\.conf$ ]]; then printf 'drop_in=%s\n' "$name"; fi
done
read_process() {
  local pid=$1 arg key value entry
  [[ "$pid" =~ ^[1-9][0-9]*$ ]] || return 1
  exec 3<"/proc/$pid/cmdline" 4<"/proc/$pid/environ" || return 1
  IFS= read -r -d '' arg <&3 && IFS= read -r -d '' arg <&3 && [[ "$arg" == run ]] || return 1
  while IFS= read -r -d '' arg <&3; do
    [[ "$arg" == -* && "$arg" != -- ]] || break
    key=${arg#-}; key=${key#-}; key=${key%%=*}
    case "$key" in
      pages|page-only|home-only|page-order|page-dwell-seconds|style|token|device-id|data-dir|node-id|node-url|node-cert-pin)
        if [[ "$arg" == *=* ]]; then value=${arg#*=}; else IFS= read -r -d '' value <&3 || return 1; fi
        emit_setting flag_ "$key" "$value"
        ;;
      verbose|no-tls) ;;
      *) return 1 ;;
    esac
  done
  while IFS= read -r -d '' entry <&4; do read_setting process_ "$entry"; done
  [[ "$pid" == "$(systemctl show "$unit" -p MainPID --value)" ]] || return 1
  if [[ /proc/$pid/exe -ef "$(command -v homepi-display)" ]]; then
    case "$capability" in
      'rotation-env-v1 gui') echo renderer=gui ;;
      'rotation-env-v1 terminal') echo renderer=terminal ;;
    esac
  fi
  echo process_verified=yes
}
read_process "$(systemctl show "$unit" -p MainPID --value)" 2>/dev/null || true
snapshot=/var/lib/homepi-display/last-known-good.json
if [[ -f "$snapshot" && ! -L "$snapshot" ]]; then
  printf 'epoch=%s\n' "$(sed -n 's/.*"source_epoch":"\([^"]*\)".*/\1/p' "$snapshot" | head -n1)"
  printf 'version=%s\n' "$(sed -n 's/.*"snapshot_version":\([0-9][0-9]*\).*/\1/p' "$snapshot" | head -n1)"
  printf 'generated=%s\n' "$(sed -n 's/.*"generated_at":"\([^"]*\)".*/\1/p' "$snapshot" | head -n1)"
fi
HOMEPI_READONLY_PROBE
