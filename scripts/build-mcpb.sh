#!/bin/bash
#
# Build the netdefense-mcp MCPB (MCP Bundle) artifact for the MCP Registry.
#
# Bundles three platform binaries into a single .mcpb zip, so the registry's
# "mcpb" package type has exactly one artifact per release (see
# https://github.com/modelcontextprotocol/registry docs/reference for the
# mcpb package type — one `identifier` + one `fileSha256` per server.json
# `packages[]` entry).
#
# Platform coverage note: the MCPB manifest format only distinguishes binary
# servers by OS (darwin/win32/linux) via server.mcp_config.platform_overrides
# — there is no per-architecture key (see modelcontextprotocol/mcpb
# MANIFEST.md). We therefore ship one binary per OS: darwin/arm64 (Apple
# Silicon), linux/amd64, windows/amd64. Intel Mac and arm64 Linux/Windows
# users should use the regular ndcli release archives (all five OS/arch
# combos, see ndcli-releases) instead of the MCPB bundle until the spec adds
# arch-level selection.
#
# Usage:
#   ./scripts/build-mcpb.sh <version> <dist-dir> <out-dir>
#
#   <version>   Release version without the leading "v" (e.g. 1.25.0);
#               matches server.json's non-"v"-prefixed `version` field.
#   <dist-dir>  Directory containing the goreleaser-built binaries (its
#               `dist/` output — this script does not build them itself).
#   <out-dir>   Directory to write the .mcpb file and its .sha256 sidecar to.
#
# Requires: zip, openssl, python3, sed, and a working `go build` (used to
# compile a throwaway host-native binary that this script introspects via a
# live MCP stdio handshake to populate the manifest's "tools" array -- see
# gen-mcpb-manifest.py).

set -euo pipefail

VERSION="${1:?usage: build-mcpb.sh <version> <dist-dir> <out-dir>}"
DIST_DIR="${2:?usage: build-mcpb.sh <version> <dist-dir> <out-dir>}"
OUT_DIR="${3:?usage: build-mcpb.sh <version> <dist-dir> <out-dir>}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
MANIFEST_TMPL="$REPO_ROOT/mcpb/manifest.json.tmpl"

if [[ ! -f "$MANIFEST_TMPL" ]]; then
    echo "error: $MANIFEST_TMPL not found" >&2
    exit 1
fi

# Locate a platform binary under dist-dir. GoReleaser's unpacked build output
# always embeds the goos/goarch as path components, so match on those plus an
# exact filename rather than relying on any specific folder-naming scheme.
find_binary() {
    local goos="$1" goarch="$2" filename="$3"
    find "$DIST_DIR" -type f -name "$filename" -path "*${goos}*" -path "*${goarch}*" 2>/dev/null | head -n1
}

DARWIN_BIN="$(find_binary darwin arm64 netdefense-mcp)"
LINUX_BIN="$(find_binary linux amd64 netdefense-mcp)"
WIN_BIN="$(find_binary windows amd64 netdefense-mcp.exe)"

for pair in "darwin/arm64:$DARWIN_BIN" "linux/amd64:$LINUX_BIN" "windows/amd64:$WIN_BIN"; do
    label="${pair%%:*}"
    path="${pair#*:}"
    if [[ -z "$path" ]]; then
        echo "error: could not find netdefense-mcp binary for $label under $DIST_DIR" >&2
        echo "       (did the goreleaser step run first and build the netdefense-mcp target?)" >&2
        exit 1
    fi
done

WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

mkdir -p "$WORKDIR/server/darwin" "$WORKDIR/server/linux" "$WORKDIR/server/win32"
install -m 0755 "$DARWIN_BIN" "$WORKDIR/server/darwin/netdefense-mcp"
install -m 0755 "$LINUX_BIN" "$WORKDIR/server/linux/netdefense-mcp"
install -m 0755 "$WIN_BIN" "$WORKDIR/server/win32/netdefense-mcp.exe"

# Populate the manifest's "tools" array from a live introspection of the
# server's own tool registry, so it can never drift from what's actually
# shipping (see gen-mcpb-manifest.py for why this works without credentials).
# This needs a binary that runs on THIS host -- the darwin/linux/win32
# binaries staged above are cross-compiled and may not match it (e.g. the
# CI runner is linux/amd64, but `make mcpb` on a dev Mac cross-compiles all
# three and none of them may be runnable there) -- so build one separately
# with a plain, unqualified `go build`.
INTROSPECT_BIN="$WORKDIR/introspect-netdefense-mcp"
(cd "$REPO_ROOT" && go build -o "$INTROSPECT_BIN" ./cmd/netdefense-mcp)

sed "s/__VERSION__/${VERSION}/g" "$MANIFEST_TMPL" > "$WORKDIR/manifest.json.tmp"
python3 "$SCRIPT_DIR/gen-mcpb-manifest.py" "$INTROSPECT_BIN" "$WORKDIR/manifest.json.tmp" "$WORKDIR/manifest.json"
python3 -m json.tool "$WORKDIR/manifest.json" > /dev/null

cp "$REPO_ROOT/mcpb/icon.png" "$WORKDIR/icon.png"

mkdir -p "$OUT_DIR"
OUT_DIR="$(cd "$OUT_DIR" && pwd)"  # resolve to absolute — we cd into $WORKDIR below to zip
MCPB_NAME="netdefense-mcp_${VERSION}.mcpb"
MCPB_PATH="$OUT_DIR/$MCPB_NAME"
rm -f "$MCPB_PATH"

# ZIP with max compression, matching the MCPB CLI's own `pack` behavior
# (an .mcpb is just a zip of manifest.json + icon + bundled server files).
(cd "$WORKDIR" && zip -X -r -9 -q "$MCPB_PATH" manifest.json icon.png server)

SHA256="$(openssl dgst -sha256 "$MCPB_PATH" | awk '{print $NF}')"
echo "$SHA256" > "${MCPB_PATH}.sha256"

echo "built: $MCPB_PATH"
echo "sha256: $SHA256"
