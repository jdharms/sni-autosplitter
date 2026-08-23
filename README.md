# SNI AutoSplitter

SNI AutoSplitter is a command-line tool that automatically triggers splits in
[LiveSplit One](https://github.com/LiveSplit/livesplit-one) by reading SNES
game memory through [SNI](https://github.com/alttpo/sni) (Super Nintendo
Interface).

It watches for game-state conditions (item pickups, room transitions, boss
kills, etc.) defined in a JSON config, and sends split/reset/pause commands to
LiveSplit One over a WebSocket connection as those conditions are met.

## How it works

```
SNES device (FX Pak Pro / RetroArch / Lua Bridge)
        │
        ▼
       SNI  (gRPC server, reads console memory)
        │
        ▼
SNI AutoSplitter  (evaluates split conditions, 60 times/sec)
        │
        ▼
   LiveSplit One  (WebSocket client, receives split events)
```

SNI AutoSplitter connects to a running SNI instance as a gRPC client to read
memory, and hosts its own WebSocket server that LiveSplit One connects to as
a client.

## Requirements

- [SNI](https://github.com/alttpo/sni) running and connected to a SNES
  device (FX Pak Pro hardware, RetroArch with a compatible core, or a
  Lua Bridge emulator such as Snes9x-rr or BizHawk)
- [LiveSplit One](https://one.livesplit.org/) configured with a WebSocket
  auto splitter connection
- Go 1.23+ (only if building from source)

## Installation

Download a prebuilt binary for your platform from the
[Releases](https://github.com/jdharms/sni-autosplitter/releases) page. Each
release archive includes the binary and the bundled `configs/` directory.

Alternatively, build from source:

```sh
git clone https://github.com/jdharms/sni-autosplitter.git
cd sni-autosplitter
make build
```

## Usage

Make sure SNI is running, then start the autosplitter:

```sh
./sni-autosplitter
```

With no arguments, it lists the available run configurations and lets you
pick one interactively. To skip the prompt, load a run directly by name:

```sh
./sni-autosplitter --run-name "alttp any% nmg"
```

In LiveSplit One, go to Settings -> Network -> Server Connection -> Connect,
and enter `http://localhost:1990`, and splits will be triggered automatically as you play.

To serve over a secure connection (required if LiveSplit One is running in Safari on a page
loaded over HTTPS, e.g. [one.livesplit.org](https://one.livesplit.org/)), pass a TLS
certificate and key. [mkcert](https://github.com/FiloSottile/mkcert) is an easy way
to generate a locally-trusted certificate for this.

```sh
./sni-autosplitter --tls-cert localhost.pem --tls-key localhost-key.pem
```

Then connect LiveSplit One to `https://localhost:1990` instead.

### Flags

Every flag can also be set via an environment variable prefixed with
`SNI_AUTOSPLITTER_` (dashes become underscores, e.g. `--sni-host` →
`SNI_AUTOSPLITTER_SNI_HOST`).

| Flag                 | Default          | Description                                                              |
|----------------------|------------------|---------------------------------------------------------------------------|
| `--run-name`          | *(none)*         | Name of the run to load; skips interactive selection                     |
| `--games-dir`         | `./configs/games`| Directory containing game configuration files                           |
| `--runs-dir`          | `./configs/runs` | Directory containing run configuration files                            |
| `--log-dir`           | `./logs`         | Directory for log files                                                  |
| `--log-level`         | `info`           | Log level (`debug`, `info`, `warn`, `error`)                             |
| `--sni-host`          | `localhost`      | SNI gRPC server host                                                     |
| `--sni-port`          | `8191`           | SNI gRPC server port                                                     |
| `--livesplit-port`    | `1990`           | WebSocket port for LiveSplit One connections                            |
| `--enable-manual-ops` | `false`          | Enable manual split/reset/pause/resume/test commands, for development   |
| `--tls-cert`          | *(none)*         | Path to TLS certificate file; enables WSS on the LiveSplit One server   |
| `--tls-key`           | *(none)*         | Path to TLS private key file; enables WSS on the LiveSplit One server   |

### Interactive commands

Once running, the tool accepts these commands on stdin:

| Command       | Description                          |
|---------------|---------------------------------------|
| `h`, `help`   | Show available commands               |
| `s`, `status` | Show current engine status            |
| `stats`       | Show detailed run statistics          |
| `q`, `quit`   | Exit                                   |

With `--enable-manual-ops`, `split`, `reset`, `pause`, `resume`, and `test`
are also available for manually driving the engine without game input.

## Configuration

Splits are defined with a two-tier config system:

- **Game configs** (`configs/games/`) define memory addresses and
  conditions for a given game, compatible with the USB2SNES autosplitter
  format.
- **Run configs** (`configs/runs/`) define a named sequence of splits (from
  a game config) for a specific speedrun category.

Currently bundled is a config for *The Legend of Zelda: A Link to the Past*
(`configs/games/alttp.json`) with five run categories. Adding support for a
new game or category just means adding a new JSON file, with hopefully
no code changes required.

See [`configs/readme.md`](configs/readme.md) for the full config format,
condition types, and validation rules.

## Development

```sh
make build          # Build the binary
make build-all       # Cross-compile for linux/windows/darwin (amd64/arm64)
make test            # Run tests
make test-coverage   # Run tests with an HTML coverage report
make fmt             # Format code
make lint            # Run golangci-lint (falls back to go vet)
make proto           # Regenerate protobuf/gRPC code from sni.proto
```

CI runs `go vet`, `go build`, and `go test` on every push and pull request
to `main`. Pushing a `v*.*.*` tag triggers a release build for all
supported platforms.
