#!/usr/bin/env bash
# Regenerate every README asset in one go.
#
#   ./docs/demo/make-assets.sh
#
# Does the whole pipeline: builds lazys3 and the seeded demo server, prepares
# a throwaway HOME at a SHORT path (the local pane shows its absolute path in
# screenshots, so /tmp/... reads far better than a deep checkout path), starts
# the server, records the GIF and the stills, and cleans up.
#
# Requires: Go, python3 with `pip install pyte pillow`.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BASE="${DEMO_BASE:-/tmp/lazys3-demo}"
PORT="${DEMO_PORT:-19093}"
BIN="$BASE/lazys3"
HOME_DIR="$BASE/home"

cleanup() {
  [[ -n "${SRV_PID:-}" ]] && kill "$SRV_PID" 2>/dev/null || true
}
trap cleanup EXIT

echo "==> building (GOOS=$(go env GOOS))"
cd "$ROOT"
go build -o "$BIN" .
go build -o "$BASE/demosrv" ./cmd/demosrv

echo "==> preparing demo HOME at $HOME_DIR"
mkdir -p "$(dirname "$HOME_DIR")"
DEMO_PORT="$PORT" bash docs/demo/build-demo-home.sh "$HOME_DIR" >/dev/null

echo "==> starting demo S3 on :$PORT"
"$BASE/demosrv" & SRV_PID=$!
for _ in $(seq 1 50); do
  if curl -sf -m 1 -o /dev/null "http://127.0.0.1:$PORT/documents?list-type=2" 2>/dev/null; then break; fi
  sleep 0.2
done

export LAZYS3_BIN="$BIN" LAZYS3_DEMO_HOME="$HOME_DIR"

echo "==> recording GIF"
python3 docs/demo/record.py gif --scene tour \
  --out docs/demo/demo.gif --scale 0.62 --colors 48 --duration 120

echo "==> recording stills"
for scene in start preview metadata search transfers help dual; do
  python3 docs/demo/record.py shot --scene "$scene" \
    --out "docs/demo/shots/$scene.png"
done

echo "==> recording light-theme still"
cp -r "$HOME_DIR" "$BASE/light-home"
python3 - "$BASE/light-home/.config/lazys3/config.yaml" <<'PY'
import sys
p = sys.argv[1]
s = open(p).read()
if "theme:" not in s:
    open(p, "w").write(s.replace("ui:", 'theme:\n  mode: "light"\n\nui:', 1))
PY
LAZYS3_DEMO_HOME="$BASE/light-home" python3 docs/demo/record.py shot --light \
  --scene preview --out docs/demo/shots/light.png

echo "==> done"
ls -lh docs/demo/demo.gif docs/demo/shots/
