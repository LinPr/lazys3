# Development

How to build, test, and regenerate the demo GIF for lazys3. End-user docs live in the [README](../README.md).

## Build from source

Requires Go 1.27+. [Task](https://taskfile.dev) is optional but wraps the common commands.

```sh
git clone https://github.com/LinPr/lazys3.git
cd lazys3
go build .          # or: task build
```

## Task targets

Defined in `Taskfile.yaml`:

| Target | What it does |
|---|---|
| `task` (default) | build, then run `./lazys3 --debug` |
| `task build` | `go build .` |
| `task lint` / `task lint-fix` | `golangci-lint` with `.golangci.yml` (optionally with `--fix`) |
| `task fmt` | `go fmt ./...` |
| `task test` / `task test-unit` | unit tests (excludes the e2e package) |
| `task test-e2e` | e2e tests against an in-process gofakes3 server |
| `task test-e2e-real` | e2e tests against a real endpoint via the `oss` shared-config profile |
| `task check-nerd-font` / `task fix-nerd-font` | find / fix broken Nerd Font glyphs in Go sources |

## Testing

```sh
task test-unit                      # unit tests (excludes e2e)
task test-e2e                       # e2e tests against an in-process gofakes3 server
LAZYS3_E2E_REAL=oss task test-e2e   # e2e against a real endpoint, using that ~/.aws profile
```

Without `task`:

```sh
go test $(go list ./... | grep -v /e2e)   # unit
go test -tags=e2e ./e2e/...               # e2e (gofakes3 by default)
```

By default the e2e suite spins up an in-process [gofakes3](https://github.com/johannesboyne/gofakes3) server, so it needs no credentials or network. Setting `LAZYS3_E2E_REAL=<profile>` (e.g. `oss`) runs the same suite against a real S3-compatible endpoint using that profile from `~/.aws/config` / `~/.aws/credentials`.

## Regenerating the demo assets

The README GIF and screenshots are recorded against a seeded in-memory S3
server (`cmd/demosrv`, `127.0.0.1:19093`), so no credentials or network are
needed. [`docs/demo/record.py`](demo/record.py) drives the TUI in a pty and
renders the frames itself with pyte + Pillow — no ffmpeg/agg required.

```sh
./docs/demo/make-assets.sh          # builds, seeds, records everything
```

That one script builds lazys3 and the demo server, prepares a throwaway
HOME under `/tmp/lazys3-demo` (a short path keeps the dual-pane screenshot
readable), starts the server, records the GIF and all stills, and cleans
up. Individual scenes can be re-recorded:

```sh
python3 docs/demo/record.py gif  --scene tour    --out docs/demo/demo.gif
python3 docs/demo/record.py shot --scene preview --out docs/demo/shots/preview.png
python3 docs/demo/record.py shot --light --scene preview --out docs/demo/shots/light.png
```

Scenes: `tour`, `start`, `preview`, `metadata`, `search`, `transfers`,
`help`, `dual`.

Requirements: `pip install pyte pillow`.

Two recording gotchas worth knowing:

- The recorder strips `NO_COLOR`/`TERM=dumb` from the child environment.
  Inheriting a shell that sets `NO_COLOR=1` makes lipgloss drop every colour
  and the output is silently monochrome.
- GIF frames are quantized with dithering **off** before saving. Pillow's
  default dithering sprays near-equal colours across flat UI areas, which
  LZW cannot compress — the same GIF grows from ~650 KiB to ~4 MiB.
