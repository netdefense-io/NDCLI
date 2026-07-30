#!/usr/bin/env python3
#
# Build the Smithery release-deploy payload (a "StdioDeployPayload" — see
# https://smithery.ai/docs/api-reference/servers/publish-a-server) from the
# already-assembled mcpb/manifest.json, so the "Publish to Smithery" step in
# release-please.yml can PUT it verbatim as the multipart `payload` field.
#
# Why this exists: the step used to hand-roll a bare
# `{"type":"stdio","runtime":"binary"}` payload, which the registry rejects
# with `400 {"error":"No values to set"}` — it parses fine but carries no
# `serverCard`, so there's nothing for the release to actually persist. This
# script fills that in from the manifest, mirroring what the official
# `smithery` CLI's `getBundleDeployPayload()` (src/lib/mcpb.ts) builds from a
# bundle's manifest.json, with one deliberate deviation: every tool gets a
# synthesized `inputSchema: {"type": "object"}`. The MCPB manifest format
# forbids `inputSchema` on tool entries (`additionalProperties: false` on
# manifest_version 0.3's tools schema), but the registry's
# `ServerCard["tools"]` schema is MCP `Tool[]`, which requires it — so the
# official CLI's raw `manifest.tools as unknown as ServerCard["tools"]` cast
# forwards tools the registry then rejects, one `expected object, received
# undefined` per tool (see https://github.com/smithery-ai/cli/issues/787,
# open/unfixed as of 2026-07 — the maintainers' own suggested fix is exactly
# this synthesized default). We only ever add the field to *this* sidecar
# payload — never to the shipped manifest.json inside the .mcpb, which must
# stay MCPB-spec-valid for every other MCPB consumer (Claude Desktop, etc).
#
# Usage:
#   gen-smithery-payload.py <manifest.json> <out-payload.json>
#
#   <manifest.json>      The final, version-substituted, tools-populated
#                         manifest that gen-mcpb-manifest.py just wrote —
#                         same file that gets zipped into the .mcpb.
#   <out-payload.json>   Where to write the release payload.
#
# Requires: python3 only (no third-party deps).

import json
import sys


def convert_user_config_to_json_schema(user_config):
    """Port of `convertMCPBUserConfigToJSONSchema` (smithery-ai/cli's
    src/lib/mcpb.ts) — kept in lockstep with that function so our
    `configSchema` matches what the official CLI would produce from the same
    manifest. Dotted keys (e.g. "a.b") nest into
    schema.properties.a.properties.b, matching MCPB's user_config
    convention for grouped settings."""
    schema = {"type": "object", "properties": {}, "required": []}
    required_fields = []

    for dot_key, opt in user_config.items():
        parts = dot_key.split(".")
        if not parts:
            continue

        current = schema
        for part in parts[:-1]:
            current.setdefault("properties", {})
            if part not in current["properties"]:
                current["properties"][part] = {"type": "object", "properties": {}}
            current = current["properties"][part]

        leaf_key = parts[-1]
        current.setdefault("properties", {})

        opt_type = opt.get("type")
        # MCPB's "directory"/"file" picker types have no JSON Schema
        # equivalent; the CLI represents the resolved path as a string.
        property_type = "string" if opt_type in ("directory", "file") else opt_type

        shared = {}
        if opt.get("title") is not None:
            shared["title"] = opt["title"]
        if opt.get("description") is not None:
            shared["description"] = opt["description"]
        if "default" in opt:
            shared["default"] = opt["default"]

        if opt.get("multiple"):
            prop = {"type": "array", "items": {"type": property_type}, **shared}
        else:
            prop = {"type": property_type, **shared}

        current["properties"][leaf_key] = prop

        if opt.get("required"):
            if len(parts) == 1:
                required_fields.append(leaf_key)
            else:
                parent_key = parts[0]
                schema.setdefault("required", [])
                if parent_key not in schema["required"]:
                    schema["required"].append(parent_key)
                parent_schema = schema["properties"][parent_key]
                for part in parts[1:-1]:
                    parent_schema = parent_schema["properties"][part]
                parent_schema.setdefault("required", [])
                if leaf_key not in parent_schema["required"]:
                    parent_schema["required"].append(leaf_key)

    # Matches the upstream function's own behavior verbatim, quirks
    # included: a top-level required list built from length-1 keys
    # overwrites schema["required"] only when non-empty, so a nested-only
    # required field (added directly above) survives whenever there are no
    # top-level required keys to replace it with.
    if required_fields:
        schema["required"] = required_fields

    return schema


def detect_runtime(manifest):
    """Port of `detectBundleRuntime` (smithery-ai/cli's src/lib/mcpb.ts),
    minus the archive-scanning fallback (irrelevant here — this runs before
    the .mcpb is zipped, and every netdefense-mcp bundle declares
    server.type explicitly)."""
    command = manifest.get("server", {}).get("mcp_config", {}).get("command") or ""
    command_basename = command.rsplit("/", 1)[-1].rsplit("\\", 1)[-1]
    if command_basename in ("bun", "bun.exe"):
        return "bun"

    server_type = manifest.get("server", {}).get("type")
    if server_type in ("python", "node", "binary"):
        return server_type

    raise ValueError(
        f"could not determine bundle runtime from manifest server.type={server_type!r} "
        f"command={command!r} — see detectBundleRuntime() in smithery-ai/cli's src/lib/mcpb.ts"
    )


def main():
    if len(sys.argv) != 3:
        print(f"usage: {sys.argv[0]} <manifest.json> <out-payload.json>", file=sys.stderr)
        return 1
    manifest_path, out_path = sys.argv[1], sys.argv[2]

    with open(manifest_path) as f:
        manifest = json.load(f)

    if not manifest.get("name") or not manifest.get("version"):
        raise ValueError("manifest.json must include name and version")

    server_card = {
        "serverInfo": {
            "name": manifest["name"],
            "version": manifest["version"],
        }
    }
    if manifest.get("tools"):
        server_card["tools"] = [
            {**tool, "inputSchema": {"type": "object"}} for tool in manifest["tools"]
        ]
    if manifest.get("prompts"):
        server_card["prompts"] = manifest["prompts"]
    if manifest.get("resources"):
        server_card["resources"] = manifest["resources"]

    payload = {
        "type": "stdio",
        "runtime": detect_runtime(manifest),
        "serverCard": server_card,
    }

    user_config = manifest.get("user_config")
    if user_config:
        payload["configSchema"] = convert_user_config_to_json_schema(user_config)

    with open(out_path, "w") as f:
        json.dump(payload, f, indent=2)
        f.write("\n")

    print(
        f"wrote Smithery release payload ({len(server_card.get('tools', []))} tools) to {out_path}",
        file=sys.stderr,
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
