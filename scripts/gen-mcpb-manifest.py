#!/usr/bin/env python3
#
# Render the final mcpb/manifest.json by filling in the "tools" array from a
# live introspection of the netdefense-mcp binary, rather than hand-maintaining
# ~130 tool entries that would rot the moment a tool is added/renamed/removed.
#
# Tool registration in internal/mcp/server.go (NewServer) is unconditional --
# every registerXTools() call runs before any auth check -- so `tools/list`
# works without NDCLI_TOKEN or a cached `ndcli auth login` session. This
# script runs the binary in a scrubbed, isolated environment (no inherited
# NDCLI_TOKEN, empty HOME/XDG_CONFIG_HOME) on purpose: if a future change
# ever makes tool registration auth-dependent, this fails loudly instead of
# silently baking a partial tool list into a release.
#
# Usage:
#   gen-mcpb-manifest.py <netdefense-mcp-binary> <manifest-tmpl-rendered> <out-manifest>
#
#   <netdefense-mcp-binary>   Path to a HOST-native (not cross-compiled)
#                             netdefense-mcp binary. build-mcpb.sh builds one
#                             specifically for this purpose since the
#                             bundle's own darwin/linux/win32 binaries are
#                             cross-compiled and may not be runnable on
#                             whatever machine executes this script.
#   <manifest-tmpl-rendered> mcpb/manifest.json.tmpl with __VERSION__
#                             already substituted (still valid JSON, with a
#                             "tools": [] placeholder).
#   <out-manifest>            Where to write the final manifest.json.
#
# Requires: python3 only (no third-party deps).

import json
import subprocess
import sys
import tempfile

PROTOCOL_VERSION = "2024-11-05"


def list_tools(binary):
    """Spawn `binary`, speak just enough MCP over stdio to call tools/list,
    and return a sorted [{"name":..., "description":...}, ...] list."""
    with tempfile.TemporaryDirectory() as home, tempfile.TemporaryDirectory() as xdg_config:
        env = {"HOME": home, "XDG_CONFIG_HOME": xdg_config, "PATH": "/usr/bin:/bin"}
        proc = subprocess.Popen(
            [binary],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            env=env,
            text=True,
            bufsize=1,
        )
        try:
            def send(msg):
                proc.stdin.write(json.dumps(msg) + "\n")
                proc.stdin.flush()

            send({
                "jsonrpc": "2.0",
                "id": 1,
                "method": "initialize",
                "params": {
                    "protocolVersion": PROTOCOL_VERSION,
                    "capabilities": {},
                    "clientInfo": {"name": "build-mcpb", "version": "0.0.0"},
                },
            })
            init_line = proc.stdout.readline()
            if not init_line:
                raise RuntimeError(f"no response to initialize; stderr: {proc.stderr.read()}")
            init_resp = json.loads(init_line)
            if "error" in init_resp:
                raise RuntimeError(f"initialize failed: {init_resp['error']}")

            send({"jsonrpc": "2.0", "method": "notifications/initialized"})
            send({"jsonrpc": "2.0", "id": 2, "method": "tools/list"})

            list_line = proc.stdout.readline()
            if not list_line:
                raise RuntimeError(f"no response to tools/list; stderr: {proc.stderr.read()}")
            list_resp = json.loads(list_line)
            if "error" in list_resp:
                raise RuntimeError(f"tools/list failed: {list_resp['error']}")

            tools = list_resp["result"]["tools"]
            if not tools:
                raise RuntimeError("tools/list returned zero tools -- refusing to bake an empty manifest")

            return sorted(
                ({"name": t["name"], "description": t.get("description", "")} for t in tools),
                key=lambda t: t["name"],
            )
        finally:
            proc.terminate()
            try:
                proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                proc.kill()


def main():
    if len(sys.argv) != 4:
        print(
            f"usage: {sys.argv[0]} <netdefense-mcp-binary> <manifest-tmpl-rendered> <out-manifest>",
            file=sys.stderr,
        )
        return 1
    binary, tmpl_path, out_path = sys.argv[1], sys.argv[2], sys.argv[3]

    tools = list_tools(binary)
    print(f"discovered {len(tools)} MCP tools", file=sys.stderr)

    with open(tmpl_path) as f:
        manifest = json.load(f)
    manifest["tools"] = tools

    with open(out_path, "w") as f:
        json.dump(manifest, f, indent=2)
        f.write("\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
