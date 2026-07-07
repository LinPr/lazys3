# Development

How to build, test, and regenerate the demo GIF for lazys3. End-user docs live in the [README](../README.md).

## Build from source

Requires Go 1.25+. [Task](https://taskfile.dev) is optional but wraps the common commands.

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

## Regenerating the demo GIF

The demo is recorded against a seeded in-memory S3 server (`cmd/demosrv`, listens on `127.0.0.1:19093`) by `docs/demo/record.py`, which drives the TUI in a pty (100x30) and writes an asciinema v2 cast:

```sh
go run ./cmd/demosrv &
python3 docs/demo/record.py                        # writes /tmp/demo.cast
agg --font-size 14 /tmp/demo.cast docs/demo.gif    # render with agg
```

The script expects the lazys3 binary at `/tmp/lazys3-demo` and a prepared demo `$HOME` under `/tmp/demo-home` (with a workspace directory and an `~/.aws` profile pointing at the demo server) — see the header of [`docs/demo/record.py`](demo/record.py) for the full setup.
