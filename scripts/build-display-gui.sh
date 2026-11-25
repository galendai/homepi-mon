#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=scripts/lib/common.sh
source "$script_dir/lib/common.sh"

require_command docker

if [[ "$#" -gt 1 ]]; then
  die "usage: $0 [output-path]"
fi

output_path="${1:-$repo_root/dist/homepi-display-gui-linux-arm64}"
if [[ "$output_path" != /* ]]; then
  output_path="$PWD/$output_path"
fi
output_dir="$(dirname -- "$output_path")"
output_name="$(basename -- "$output_path")"
mkdir -p "$output_dir"

commit="$(resolve_commit)"
build_date="$(resolve_build_date)"
ldflags="$(build_ldflags "$commit" "$build_date")"

log "build homepi-display with LVGL v9.6.0 for linux/arm64"
docker run --rm \
  -e "HOMEPILDFLAGS=$ldflags" \
  -e "OUTPUT_NAME=$output_name" \
  -e "HOST_UID=$(id -u)" \
  -e "HOST_GID=$(id -g)" \
  -v "$repo_root:/src:ro" \
  -v "$output_dir:/output" \
  -w /src \
  golang:1.26.5-bookworm \
  bash -euo pipefail -c '
    export DEBIAN_FRONTEND=noninteractive
    apt-get update
    apt-get install -y --no-install-recommends cmake gcc-aarch64-linux-gnu g++-aarch64-linux-gnu
    rm -rf /var/lib/apt/lists/*

    git clone --quiet --filter=blob:none --no-checkout https://github.com/lvgl/lvgl.git /tmp/lvgl
    git -C /tmp/lvgl checkout --quiet 80ca777e37a2b176770726a02e07a6fb79ef0b39

    cmake -S /tmp/lvgl -B /tmp/lvgl-build \
      -DCMAKE_BUILD_TYPE=Release \
      -DCMAKE_SYSTEM_NAME=Linux \
      -DCMAKE_SYSTEM_PROCESSOR=aarch64 \
      -DCMAKE_C_COMPILER=aarch64-linux-gnu-gcc \
      -DCMAKE_CXX_COMPILER=aarch64-linux-gnu-g++ \
      -DLV_BUILD_CONF_PATH=/src/internal/guirender/lv_conf.h \
      -DCONFIG_LV_BUILD_DEMOS=OFF \
      -DCONFIG_LV_BUILD_EXAMPLES=OFF \
      -DCONFIG_LV_USE_THORVG=OFF \
      -DCONFIG_LV_USE_THORVG_INTERNAL=OFF \
      -DLV_BUILD_INSTALL=OFF \
      -DLV_USE_FIND_PACKAGE=OFF \
      -DLV_USE_PKG_CONFIG=OFF \
      -DLV_FETCH_DEPENDENCIES=OFF \
      -DBUILD_SHARED_LIBS=OFF
    cmake --build /tmp/lvgl-build --parallel "$(nproc)"

    export GOOS=linux GOARCH=arm64 CGO_ENABLED=1
    export CC=aarch64-linux-gnu-gcc
    export CGO_CFLAGS="-I/tmp/lvgl/include -O2"
    export CGO_LDFLAGS="-L/tmp/lvgl-build"
    export GOCACHE=/tmp/go-cache GOMODCACHE=/tmp/go-mod-cache
    go build -trimpath -ldflags "$HOMEPILDFLAGS" \
      -o "/output/.$OUTPUT_NAME.tmp" ./cmd/homepi-display
    chown "$HOST_UID:$HOST_GID" "/output/.$OUTPUT_NAME.tmp"
    mv -f "/output/.$OUTPUT_NAME.tmp" "/output/$OUTPUT_NAME"
  '

log "GUI display binary is ready at $output_path"
