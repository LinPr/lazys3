#!/usr/bin/env bash
# Build a throwaway HOME for recording the demo assets.
#
# It points a single "demo" profile at cmd/demosrv (127.0.0.1:19093, seeded
# with sample buckets) and configures a per-profile start location, so the
# recording exercises the feature the README advertises without needing any
# real credentials.
set -euo pipefail

HOME_DIR="${1:-.cache/demo/home}"
PORT="${DEMO_PORT:-19093}"

rm -rf "$HOME_DIR"
mkdir -p "$HOME_DIR/.aws" "$HOME_DIR/.config/lazys3" "$HOME_DIR/workspace/docs"

cat > "$HOME_DIR/.aws/config" <<EOF
[default]
region = us-east-1

[profile demo]
region = cn-hangzhou
endpoint_url = http://127.0.0.1:${PORT}
EOF

cat > "$HOME_DIR/.aws/credentials" <<'EOF'
[demo]
aws_access_key_id = demo
aws_secret_access_key = demo
EOF

cat > "$HOME_DIR/.config/lazys3/config.yaml" <<'EOF'
# Config used by the README recordings.
ui:
  default_sort: "name"

local:
  # Point the local pane at the demo workspace so the dual-pane scene shows
  # a small, readable directory instead of the recorder's cwd.
  start_dir: "~/workspace"

profiles:
  demo:
    bucket: "documents"
    prefix: ""
EOF

# A few local files so the dual-pane scene has something to show.
printf '# Lazys3 demo\nlocal pane content\n' > "$HOME_DIR/workspace/docs/notes.md"
printf 'alpha\nbeta\ngamma\n' > "$HOME_DIR/workspace/docs/todo.txt"

echo "demo HOME ready: $HOME_DIR"
