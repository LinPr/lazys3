# lazys3

English | [简体中文](README.zh-CN.md)

**An S3 browser for your terminal.** Navigate buckets and prefixes like a
file manager, preview objects without downloading them, move files between
local and remote with one keystroke, and start straight inside a bucket even
when your credentials are not allowed to list them.

![lazys3 demo](docs/demo/demo.gif)

*Browsing, previewing without downloading, searching a whole bucket, and
dual-pane local ⇄ remote — recorded from a real session.*

```sh
go install github.com/LinPr/lazys3@latest
```

No Go toolchain? Grab a static binary from
[**Releases**](https://github.com/LinPr/lazys3/releases/latest) —
Linux, macOS and Windows, amd64 and arm64.

Works with AWS S3, Aliyun OSS, Huawei OBS, Tencent COS, MinIO, Ceph — anything
speaking the S3 protocol.

---

## At a glance

<table>
<tr>
<td width="50%">

**Open a bucket/prefix directly**

Per-profile start locations skip `ListBuckets` entirely, so bucket-scoped
credentials work.

</td>
<td width="50%">

**Preview before download**

`p` streams the first 256 KiB of any text file into a scrollable overlay.

</td>
</tr>
<tr>
<td><img src="docs/demo/shots/start.png" alt="Per-profile start location"></td>
<td><img src="docs/demo/shots/preview.png" alt="Content preview overlay"></td>
</tr>
<tr>
<td>

**Full object metadata**

`m` shows Content-Type, ETag, storage class, SSE, checksums, user metadata.

</td>
<td>

**Find anything in the bucket**

`F` searches recursively and jumps to the first match.

</td>
</tr>
<tr>
<td><img src="docs/demo/shots/metadata.png" alt="Metadata overlay"></td>
<td><img src="docs/demo/shots/search.png" alt="Recursive search"></td>
</tr>
<tr>
<td>

**Live transfers**

`t` lists every upload, download and sync with progress; `x` cancels.

</td>
<td>

**Local ⇄ remote, side by side**

`l` opens the dual-pane view; `tab` switches panes, and file ops follow focus.

</td>
</tr>
<tr>
<td><img src="docs/demo/shots/transfers.png" alt="Transfers overlay"></td>
<td><img src="docs/demo/shots/dual.png" alt="Dual-pane mode"></td>
</tr>
</table>

## Features

**Browse**

- Multi-profile: reads every profile from `~/.aws/config` / `~/.aws/credentials`, honouring a custom `endpoint_url` per profile
- Path-style addressing is chosen automatically: AWS S3 and Aliyun OSS use virtual-host style, other custom endpoints get path-style (OSS rejects path-style, so it is special-cased)
- Navigate profiles → buckets → objects with `enter` / `backspace`; prefixes behave like directories
- Content preview (`p`): the first 256 KiB of the highlighted file in a floating, scrollable overlay, with binary and empty-file detection
- Metadata overlay (`m`): every populated field of the highlighted item — full `HeadObject` for objects, region/versioning for buckets, path/permissions/owner/timestamps/symlink target for local entries, config paths for profiles
- Filter (`/`), sort by name/size/time (`o` / `O`) and multi-select (`space`, `a`) in every list
- Recursive search (`F`): find a key anywhere in the bucket and jump to its prefix

**Transfer**

- Dual-pane mode (`l`): local filesystem on one side, S3 on the other; `tab` switches focus and file operations act on the focused pane
- Upload (`u`) and download (`d`), including recursive folder transfers through the sync engine
- Directory sync (`s`): local ⇄ S3 and S3 ⇄ S3, prefilled in dual-pane mode
- Live transfers overlay (`t`) with progress bars, newest first; `x` cancels a running transfer
- Go to path (`g`): jump straight to an `s3://bucket/prefix/` or a local directory

**Manage**

- Create buckets and local directories (`B`), rename (`r`), copy (`c`)
- Recursive delete (`D`) for prefixes and local directories, plus empty-bucket deletion — always behind a confirm modal
- Multipart cleanup (`P`): list and abort interrupted uploads, whose parts never appear in a listing but are billed as storage
- Confirm modals with Yes/No buttons: `tab`/arrows move the highlight, `enter` confirms, `y`/`n`/`esc` always work

**Versioning & sharing**

- Presigned download URLs (`Y`) with configurable expiry (1s..168h, default 1h), copied straight to the clipboard
- Presigned upload URLs (`U`): hand someone a PUT link so they can write into the current prefix without credentials
- Object versions (`v` in the object list): download, restore or delete a specific version
- Bucket versioning toggle (`v` in the bucket list): Enabled ⇄ Suspended
- Yank (`y`): the `s3://` URI of a bucket/object, or the absolute path of a local entry

**Startup & configuration**

- Start inside a bucket/prefix at launch — per profile in `config.yaml`, or with `lazys3 s3://bucket/prefix/` — with no `ListBuckets` call at all
- Per-profile `endpoint` / `region` overrides, so a profile can be retargeted without editing `~/.aws/config`
- Warm dark and light themes (`theme.mode`), plus per-colour overrides and optional Nerd Font icons

<p align="center">
  <img src="docs/demo/shots/light.png" alt="Light theme" width="70%">
</p>
- A broken config file never crashes the TUI — invalid values fall back to defaults with a log line

## Keybindings

Press `?` inside lazys3 to see this list as a scrollable overlay.

### Global

| Key | Action |
|---|---|
| `q` | quit lazys3 |
| `ctrl+c` | force quit |
| `?` | toggle the help overlay |
| `t` | toggle the live transfers overlay (newest first; `enter`: per-file sync detail, `←`/`→` scroll wide tables) |
| `x` | cancel the most recent running transfer (transfers overlay: the highlighted one) |
| `l` | toggle dual-pane layout (local ⇄ remote, needs ≥80 cols) |
| `tab` | switch focus between remote and local panes (dual-pane) |
| `p` | preview file content (floating overlay, first 256 KiB) |
| `m` | object/file metadata (floating overlay; buckets and profiles too) |
| `enter` / `→` | open selected (profile → buckets → objects) |
| `backspace` / `←` | go back one level |
| `↑`/`k`, `↓`/`j` | move the list cursor (also scrolls the `?`/`t`/`v`/`p`/`m` overlays) |

### Remote pane (S3)

| Key | Action |
|---|---|
| `d` | download selected object(s); dual-pane: folders too, into the local directory |
| `u` | upload a local file to the current prefix (single-pane only; dual-pane hints to press `tab` — uploads run with local focus) |
| `D` | delete selected object(s); folders recursively (permanent) / empty bucket (bucket list) |
| `r` | rename selected object (copy + delete) |
| `c` | copy selected object to `s3://bucket/key` (dual-pane: to the local pane) |
| `B` | make bucket (bucket list; the object list only hints) |
| `s` | sync directory (local ⇄ s3, s3 ⇄ s3; dual-pane prefills both sides) |
| `y` | yank the highlighted bucket/object `s3://` URI to the clipboard |
| `U` | generate a presigned upload (PUT) URL for someone else to write here |
| `F` | recursive search: find a key anywhere in the bucket, jump to it |
| `P` | list and abort interrupted multipart uploads (billed, invisible) |
| `Y` | generate presigned share URL (object files only) |
| `v` | object versions (object list) / toggle bucket versioning Enabled ⇄ Suspended (bucket list) |
| `g` | go to path: `s3://bucket/prefix/` switches bucket, `/path` from the bucket root, `rel/path` from here |

### Local pane

| Key | Action |
|---|---|
| `u` | upload selection to the remote bucket/prefix (folders sync recursively) |
| `c` | copy selection to the remote pane (same as `u`) |
| `d` | hints to press `tab` (downloads run with remote focus) |
| `D` | delete selection (permanent, no trash; directories recursive) |
| `r` | rename the highlighted entry (same directory) |
| `B` | create a directory |
| `s` | sync directory: local pane → remote pane (prefilled, editable) |
| `y` | yank the highlighted entry's absolute path to the clipboard |
| `g` | go to directory (absolute, `~` or relative to the current one) |

### Selection & filter

| Key | Action |
|---|---|
| `space` | toggle selection on the highlighted item (✔ mark) |
| `a` | invert selection (select all ↔ none) |
| `/` | filter the focused list (`enter` applies, `esc` clears) |
| `o` | cycle sort field (name → size → time) |
| `O` | reverse sort direction |

### Overlays

| Key | Action |
|---|---|
| `pgup` / `pgdn` | scroll one page |
| `g` / `G` | jump to top / bottom (help, transfers, preview, metadata) |
| `esc` | close the overlay (lists: clear filter; modal: cancel) |

## Install

### Prebuilt binary (no Go toolchain needed)

Every [release](https://github.com/LinPr/lazys3/releases/latest) ships
standalone binaries. They are built with `CGO_ENABLED=0`, so the Linux ones
are statically linked and run on any distribution regardless of its glibc
version.

| OS | Architecture | Asset |
|---|---|---|
| Linux | x86-64 | `lazys3-linux-amd64` |
| Linux | arm64 | `lazys3-linux-arm64` |
| macOS | Apple Silicon | `lazys3-darwin-arm64` |
| macOS | Intel | `lazys3-darwin-amd64` |
| Windows | x86-64 | `lazys3-windows-amd64.exe` |
| Windows | arm64 | `lazys3-windows-arm64.exe` |

**Linux / macOS**

```sh
# 1. download the asset for your platform from the table above, plus checksums
curl -fLO https://github.com/LinPr/lazys3/releases/latest/download/lazys3-linux-amd64
curl -fLO https://github.com/LinPr/lazys3/releases/latest/download/checksums.txt

# 2. verify (skip on macOS, which ships `shasum` instead — see below)
sha256sum --check --ignore-missing checksums.txt

# 3. install
chmod +x lazys3-linux-amd64
sudo mv lazys3-linux-amd64 /usr/local/bin/lazys3   # or: ~/.local/bin/
lazys3 --version
```

Keep the downloaded file's original name until after verification:
`checksums.txt` lists assets by name, so renaming first makes
`--ignore-missing` skip every entry.

On macOS the same check is `shasum -a 256 -c --ignore-missing checksums.txt`
(`sha256sum` is a GNU coreutils tool and is not installed by default).

The `latest/download/` URL always resolves to the newest release, so these
commands keep working after upgrades.

macOS refuses to run unsigned binaries downloaded from the internet. If you
get *"cannot be opened because the developer cannot be verified"*, clear the
quarantine flag once:

```sh
xattr -d com.apple.quarantine ./lazys3-darwin-arm64   # the file you downloaded
```

**Windows** — download the `.exe` for your architecture from the
[releases page](https://github.com/LinPr/lazys3/releases/latest), rename it to
`lazys3.exe`, and put it in a directory on `%PATH%` (for example
`%LOCALAPPDATA%\Microsoft\WindowsApps`).

### With Go

Requires Go 1.27+.

```sh
go install github.com/LinPr/lazys3@latest
```

`go install` puts the binary in `$(go env GOPATH)/bin`, which is on `PATH`
for most setups. `lazys3 --version` reports the installed tag (read from Go's
build info, since `go install` cannot pass linker flags).

### From source

```sh
git clone https://github.com/LinPr/lazys3.git
cd lazys3
go build .          # or: task build
```

### Shell completion

`lazys3` completes its flags for bash, zsh, fish and PowerShell:

```sh
lazys3 completion zsh > "${fpath[1]}/_lazys3"     # zsh
lazys3 completion bash > /etc/bash_completion.d/lazys3
```

## Quick start

lazys3 reads the standard AWS shared config. Create `~/.aws/config` with one profile per storage account:

```ini
# ~/.aws/config
[default]
region = us-east-1

[profile oss]
region = cn-hangzhou
endpoint_url = https://oss-cn-hangzhou.aliyuncs.com
```

Credentials live in `~/.aws/credentials`:

```ini
# ~/.aws/credentials
[default]
aws_access_key_id = YOUR_ACCESS_KEY_ID
aws_secret_access_key = YOUR_SECRET_ACCESS_KEY

[oss]
aws_access_key_id = YOUR_ACCESS_KEY_ID
aws_secret_access_key = YOUR_SECRET_ACCESS_KEY
```

The `default` profile targets AWS S3; any profile with an `endpoint_url` targets that S3-compatible service (MinIO, Aliyun OSS, ...). Path-style vs. virtual-host addressing is chosen automatically per endpoint.

Then launch it:

```sh
lazys3
```

Pick a profile, press `enter` to list buckets, and press `?` any time for the full keymap.

If your credentials are bucket-scoped and `s3:ListAllMyBuckets` is denied, skip the bucket picker entirely:

```sh
lazys3 --profile oss s3://my-bucket/logs/      # profile + location from the CLI
```

## Configuration

lazys3 reads `$XDG_CONFIG_HOME/lazys3/config.yaml` (default `~/.config/lazys3/config.yaml`). On first run a commented template is written so the knobs are discoverable. Every key is optional; invalid values fall back to the built-in defaults with a log line.

```yaml
# lazys3 configuration.
# All keys are optional; the commented values show the built-in defaults.

theme:
  # mode: "dark"                   # built-in palette: dark | light
  # Colors are hex strings: "#rgb" or "#rrggbb"; the keys below override
  # individual entries of the chosen palette.
  # focused_border: "#D9A441"    # border of the focused pane
  # unfocused_border: "#4A4238"  # border of the unfocused pane (dual-pane mode)
  # title_fg: "#1C1917"          # list/status chip foreground
  # title_bg: "#D9A441"          # list/status chip background
  # status_error_fg: "#ffffff"   # status-bar error text
  # selected_fg: ""              # highlighted list row foreground

ui:
  # nerd_font: false             # render Nerd Font file icons (needs a patched font)
  # default_sort: "name"         # initial sort field: name | size | time
  # sort_desc: false             # sort descending by default

local:
  # start_dir: ""                # local pane start directory, "~" ok (default: process cwd)

s3:
  # Client-level knobs, mostly for self-hosted / older S3-compatible services.
  # max_retries: 3               # SDK retry attempts (default: SDK's 3)
  # use_list_objects_v1: false   # legacy ListObjects, for services without V2
  # insecure_skip_verify: false  # skip TLS verification (self-signed endpoint)
  # no_sign_request: false       # anonymous requests (public buckets, read-only)

profiles:
  # Per-profile startup location. When the profile is selected, lazys3 opens
  # this bucket/prefix directly instead of calling ListBuckets, which
  # bucket-scoped credentials often deny. The prefix is optional and always
  # treated as a directory prefix ("logs" lists "logs/").
  # oss:
  #   bucket: "my-bucket"
  #   prefix: "some/prefix/"
  #   endpoint: "https://s3.internal.example.com"  # optional: override endpoint_url
  #   region: "cn-hangzhou"                        # optional: override region
```

Flags:

- `--config <path>` — read the lazys3 config from an explicit file instead of the default location (`lazys3 --config ./lazys3.yaml`); the file must exist, be readable and parse as YAML.
- `--aws-config <path>` — AWS shared config file (`lazys3 --aws-config ~/work/aws-config`); precedence: flag > `AWS_CONFIG_FILE` > `~/.aws/config`.
- `--aws-credentials <path>` — AWS shared credentials file (`lazys3 --aws-credentials ~/work/aws-credentials`); precedence: flag > `AWS_SHARED_CREDENTIALS_FILE` > `~/.aws/credentials`.
- `s3.max_retries` / `use_list_objects_v1` / `insecure_skip_verify` / `no_sign_request` — client-level knobs for self-hosted or older S3 services. `use_list_objects_v1` is what makes appliances that never implemented ListObjectsV2 listable at all; `insecure_skip_verify` turns TLS verification off (self-signed endpoints only — it logs a warning).
- `--profile <name>` — select this profile as soon as the AWS shared config is read, skipping the profile picker (`lazys3 --profile oss`).
- `lazys3 [s3://bucket/prefix/]` — optional positional start location. It overrides the selected profile's configured start location for this run; the prefix is normalized to a directory prefix.

Notes:

- Older versions used `config.toml`; it is no longer read — rename it to `config.yaml` and convert the content to YAML syntax (a reminder is printed on stderr, and no template overwrites it).
- `local.start_dir` accepts `~` and relative paths (resolved against the launch directory); it must be an existing directory or it is ignored.
- A profile start location or positional URI opens the object list without calling `ListBuckets`. The bucket list is fetched the first time you back out to it, so an access-denied error (if any) appears only when you actually open that list.
- `--profile` must name a profile present in the resolved AWS shared config; otherwise the profile picker stays open and the status bar reports the error.
- `ui.transfer_panel_height` from older versions is deprecated and ignored — the bottom transfer panel was replaced by the full-screen transfers overlay (`t`). Old config files still load without error.

## License

MIT — see [LICENSE](LICENSE).

---

Contributing / development (build, tests, demo GIF): [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md)
、