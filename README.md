# NetDefense CLI (ndcli)

The official command-line interface for the [NetDefense](https://netdefense.io) platform.

## Installation

### Homebrew (macOS / Linux)

```bash
brew install netdefense-io/tap/ndcli
```

### Scoop (Windows)

```powershell
scoop bucket add netdefense https://github.com/netdefense-io/scoop-bucket
scoop install ndcli
```

### Go Install

```bash
go install github.com/netdefense-io/NDCLI/cmd/ndcli@latest
```

### Binary Downloads

Pre-built binaries for macOS, Linux, and Windows are available on the
[Releases](https://github.com/netdefense-io/ndcli-releases/releases) page.

## Building from Source

Requires Go 1.24 or later.

```bash
git clone https://github.com/netdefense-io/NDCLI.git
cd NDCLI
make build
```

The binaries are placed in `bin/`:

```bash
./bin/ndcli --help
```

### Build Targets

| Target | Description |
|--------|-------------|
| `make build` | Build the ndcli binary |
| `make build-mcp` | Build the netdefense-mcp binary |
| `make build-all` | Cross-compile for all platforms |
| `make test` | Run tests |
| `make install` | Install to `$GOPATH/bin` |

## Getting Started

```bash
# Authenticate with your NetDefense account
ndcli auth login

# Set your default organization
ndcli config set organization <your-org>

# List devices
ndcli device list

# View organization details
ndcli org describe <your-org>
```

Run `ndcli --help` for the full list of commands.

## License

This project is licensed under the Apache License 2.0 - see the [LICENSE](LICENSE) file for details.
