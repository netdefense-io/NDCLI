# ndcli

**Manage a fleet of OPNsense firewalls from your terminal — or let any MCP-compatible AI agent do it, inside guardrails it can't drive around.**

`ndcli` is the command-line interface and MCP server for [NetDefense for OPNsense](https://netdefense.io): list devices, push config, run health checks, roll out firmware, manage backups, and orchestrate an entire organizational unit or fleet from one command — scriptable end to end, no web app required.

[![License: Apache 2.0](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)

## Give an AI agent your firewall fleet — safely

The second binary in this repo, `netdefense-mcp`, exposes **129 [MCP](https://modelcontextprotocol.io) tools** covering every domain `ndcli` does — devices, organizations, OUs, sync, tasks, run commands, schedules, snippets, software policies, templates, networks, variables, backups, and persistent remote consoles. Point Claude, or any other MCP-compatible client, at it and it can inspect and operate your entire fleet the same way a human operator would from the CLI — not a single device, the whole organization.

That capability only matters if it can't be pointed at your own foot, so:

- **Destructive tools require an explicit confirmation.** Calls to `ndcli.device.approve_all`, `ndcli.device.remove`, `ndcli.org.delete`, and every other tool that mutates or deletes fleet-wide state return a dry-run preview unless the caller passes `confirm=true` — an agent can't wipe a fleet on its first hallucinated call.
- **It structurally cannot mint non-expiring credentials.** `ndcli.auth.token_create` only accepts bounded lifetimes (30–365 days) over MCP; a `never`-expiring personal access token can only be created interactively on the CLI — the MCP tool schema doesn't offer the option, and the handler rejects it if forced.
- **Secrets never enter the AI-facing toolset.** Backup encryption keys and storage credentials have no MCP tool and no MCP-exposed field. They're reachable only via `ndcli backup encryption-key` and `ndcli backup config set` on the CLI.
- **Auth, account deletion, and long-lived interactive sessions stay CLI-only.** Browser-based login, `ndcli auth delete`, and `ndcli device connect` (the SSH-like device tunnel) aren't exposed to MCP at all — they need a human in the loop by design.

Point any MCP-compatible agent at the `netdefense-mcp` binary (config shape varies by client; this is the common form):

```json
{
  "mcpServers": {
    "netdefense": {
      "command": "netdefense-mcp",
      "env": { "NDCLI_TOKEN": "ndpat_..." }
    }
  }
}
```

Full tool list and setup: https://netdefense.io/docs/mcp/

## Install

### Homebrew (macOS / Linux)

```bash
brew install netdefense-io/tap/ndcli
```

### Scoop (Windows)

```powershell
scoop bucket add netdefense https://github.com/netdefense-io/scoop-bucket
scoop install ndcli
```

### Binary downloads

Pre-built archives for macOS (amd64/arm64), Linux (amd64/arm64), and Windows (amd64) are on the [ndcli-releases](https://github.com/netdefense-io/ndcli-releases/releases) page. Each archive contains both `ndcli` and `netdefense-mcp`.

### From source

Requires Go 1.24+.

```bash
git clone https://github.com/netdefense-io/NDCLI.git
cd NDCLI
make build build-mcp
```

## Usage

```bash
ndcli auth login                              # interactive login, opens a browser
ndcli config set organization acme-corp

ndcli device list                             # see the fleet
ndcli device approve edge-fw-03               # bring a newly-registered device under management
ndcli run ping --device edge-fw-03 --host 1.1.1.1
ndcli sync apply --ou branch-offices base-policy
ndcli device health edge-fw-03 -f detailed
```

Every command supports `-f table|simple|detailed|json`. Run `ndcli --help` for the full surface.

## Security model

NDAgent — the daemon that runs on the firewall — understands exactly **ten named operations**: ping, config sync, config pull, restart, reboot, shutdown, backup, remote session, plugin install, firmware upgrade. It rejects everything else. There is no "run this shell command" operation, at any layer.

Every command dispatched to a device is **Ed25519-signed**, covering the operation, the target device, an expiry, and a strictly-increasing per-device sequence number — so a command can't be replayed, reordered, or forged by a compromised relay in the middle. The agent **opens no listening ports**; it only dials out over TLS, so there's nothing on the device for an attacker to scan or connect to.

Full writeup, threat model, and what you can verify yourself against this source: **https://netdefense.io/security**

## What's open source

NDAgent and NDCLI (this repo) — the daemon that runs on your firewall, and the CLI/MCP server you run from your workstation — are open source under **Apache-2.0**. Read them, build them yourself, audit the signing and the agent's operation set directly; none of the security model above asks you to take our word for it.

The control plane that coordinates a fleet — the API, the web app, scheduling, and the device relay — is closed-source SaaS, with a free tier for personal use. See [netdefense.io/pricing](https://netdefense.io/pricing) for details.

## Links

- Docs: https://netdefense.io/docs/ndcli/getting-started/
- MCP setup: https://netdefense.io/docs/mcp/
- Security model: https://netdefense.io/security
- NDAgent (the firewall-side agent, also open source): https://github.com/netdefense-io/NDAgent
- https://netdefense.io

## License

Apache License 2.0 — see [LICENSE](LICENSE).
