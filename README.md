# lazys3

lazys3 is a lightweight command-line tool for managing and interacting with S3-compatible object storage services. Designed for efficiency and simplicity, lazys3 allows you to perform common S3 operations directly from your terminal.

## Features

- List, upload, download, and delete objects in S3 buckets
- Support for multiple S3-compatible storage providers
- Simple and intuitive terminal user interface (TUI)
- Profile management for different S3 accounts
- Configurable via INI files

## Installation

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
