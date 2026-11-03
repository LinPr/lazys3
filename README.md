# lazys3

English | [简体中文](README.zh-CN.md)

**An S3 browser for your terminal.** Navigate buckets and prefixes like a
file manager, preview objects without downloading them, move files between
local and remote with one keystroke, and start straight inside a bucket even
when your credentials are not allowed to list them.

![lazys3 demo](docs/demo/demo.gif)

```
go install github.com/LinPr/lazys3@latest
```

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

Requires Go 1.27+.

```sh
go install github.com/LinPr/lazys3@latest
```

Or build from source:

```sh
git clone https://github.com/LinPr/lazys3.git
cd lazys3
go build .          # or: task build
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