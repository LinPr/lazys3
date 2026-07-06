# lazys3

lazys3 is a lightweight command-line tool for managing and interacting with S3-compatible object storage services. Designed for efficiency and simplicity, lazys3 allows you to perform common S3 operations directly from your terminal.

## Features

- List, upload, download, and delete objects in S3 buckets
- Support for multiple S3-compatible storage providers
- Simple and intuitive terminal user interface (TUI)
- Profile management for different S3 accounts
- Configurable via INI files

## Installation

### Download from release page
https://github.com/LinPr/lazys3/releases

### Build from source
```sh
git clone https://github.com/LinPr/lazys3.git
cd lazys3
go build -o lazys3 main.go
```

Or use the provided Taskfile:

```sh
task build
```

## Usage

Start the TUI:

```sh
./lazys3
```

### Common Commands

- Navigate buckets and objects using keyboard shortcuts
- Upload and download files with simple key bindings
- Switch between different S3 profiles

## Configuration

### lazys3 config file

lazys3 reads `$XDG_CONFIG_HOME/lazys3/config.toml` (default `~/.config/lazys3/config.toml`). A commented template is written on first run. All keys are optional; invalid values fall back to the defaults.

```toml
[theme]
focused_border = "#20e71c"    # border of the focused pane
unfocused_border = "#555555"  # border of the unfocused pane (dual-pane mode)
title_fg = "#e39f00"          # status-bar profile chip foreground
title_bg = "#444745"          # status-bar profile chip background
status_error_fg = "#ffffff"   # status-bar error text
selected_fg = ""              # highlighted list row foreground

[ui]
nerd_font = false             # render Nerd Font file icons (needs a patched font)
default_sort = "name"         # initial sort field: name | size | time
sort_desc = false             # sort descending by default
transfer_panel_height = 6     # transfer panel rows, 4..10

[local]
start_dir = ""                # local pane start directory, "~" ok (default: process cwd)
```

### AWS credentials

Configure your S3 credentials and endpoints in `~/.aws/config` and `~/.aws/credentials`. Example:

```ini
[default]
region = us-east-1
endpoint = https://s3.amazonaws.com

[profile oss]
endpoint_url = http://oss-cn-hangzhou.aliyuncs.com
region = cn-hangzhou
```

```ini
[default]
aws_access_key_id = YOUR_ACCESS_KEY
aws_secret_access_key = YOUR_SECRET_KEY

[profile oss]
aws_access_key_id = YOUR_ACCESS_KEY
aws_secret_access_key = YOUR_SECRET_KEY
```

## Supported Providers
- Amazon S3
- MinIO
- Other S3-compatible services

## License

This project is licensed under the MIT License.
