#!/bin/bash
#
# Render server.json from server.json.tmpl for the MCP Registry.
#
# server.json cannot be a static, hand-maintained file: its `version` and
# `packages[0].fileSha256` fields change on every release (fileSha256 is the
# hash of that release's .mcpb artifact, built by scripts/build-mcpb.sh). This
# script fills in server.json.tmpl's placeholders and writes the result to
# server.json at the repo root — run it from the release pipeline, after the
# .mcpb bundle has been built and its sha256 computed.
#
# Usage:
#   ./scripts/render-server-json.sh <version> <mcpb-sha256>
#
#   <version>      Release version without the leading "v" (e.g. 1.25.0).
#   <mcpb-sha256>  sha256 of the .mcpb bundle for this release (64 lowercase
#                  hex chars — see scripts/build-mcpb.sh's *.mcpb.sha256
#                  sidecar output).
#
# Requires: python3 (for JSON validation).

set -euo pipefail

VERSION="${1:?usage: render-server-json.sh <version> <mcpb-sha256>}"
SHA256="${2:?usage: render-server-json.sh <version> <mcpb-sha256>}"

if [[ ! "$SHA256" =~ ^[a-f0-9]{64}$ ]]; then
    echo "error: sha256 must be 64 lowercase hex chars, got: $SHA256" >&2
    exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
TMPL="$REPO_ROOT/server.json.tmpl"
OUT="$REPO_ROOT/server.json"

if [[ ! -f "$TMPL" ]]; then
    echo "error: $TMPL not found" >&2
    exit 1
fi

sed -e "s/__VERSION__/${VERSION}/g" -e "s/__MCPB_SHA256__/${SHA256}/g" "$TMPL" > "$OUT"
python3 -m json.tool "$OUT" > /dev/null

echo "rendered: $OUT (version=$VERSION, fileSha256=$SHA256)"
