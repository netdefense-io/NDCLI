# netdefense-connect

The companion of `ndcli`, `netdefense-mcp` and `netdefense` for remote
sessions on the **direct-peer + encrypted relay** transport. An organization
chooses its transport; the websocket relay is the default and does not use
this program.

When a session uses it, the binary that opened the session starts one
`netdefense-connect` process for it, from the directory it is installed in,
and stops it when the session ends. The session is encrypted end to end
between this machine and the device. It runs peer to peer when the two
networks allow it and through the organization's relay otherwise, and the
relay sees only ciphertext either way.

It is not a command to run by hand: it takes no arguments (only `--version`
and `--help`), and reads its instructions from the program that starts it.

## How it is started

Three pipes, no socket, no file, nothing on the command line or in the
environment:

| Pipe | Carries |
| --- | --- |
| stdin | one configuration message (a 4-byte big-endian length, then JSON), then the session's bytes to the device |
| stdout | the session's bytes from the device |
| stderr | one JSON event per line: `hello`, `connected`, `path`, `closed`, `error` |

`hello` comes first, before any key is read, with the version and the tunnel
generation. The binary that starts it refuses one whose version is not its
own, so the two are installed and upgraded together. Closing stdin ends the
session; a companion never outlives the program that started it.

It carries bytes and nothing more. The session's keys are derived, and the
frames of the session written and read, by the program that starts it.

## Building

It is a Go module of its own, so its dependencies stay out of the main
module's. From the repository root:

```bash
make build-connect        # bin/netdefense-connect, beside the other binaries
make test-connect
```

`make build-connect` builds what a release ships: cgo off and the tags of
`build-tags.txt`, which leave out every optional feature of its dependencies
that it does not use.
