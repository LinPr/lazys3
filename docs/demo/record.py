#!/usr/bin/env python3
"""Record a lazys3 demo session into an asciinema cast v2 file.

Spawns the TUI in a pty (80x24), feeds a scripted key sequence with
human-ish pacing, and captures the raw output stream with timestamps.
Rendered to GIF afterwards with `agg`.
"""
import json
import os
import pty
import select
import sys
import time

CAST = "/tmp/demo.cast"
COLS, ROWS = 100, 30

# (delay_before_seconds, keys) — keys are written raw to the pty.
SCRIPT = [
    # (regenerate with: go run ./cmd/demosrv &  then  python3 docs/demo/record.py
    #  and render:      agg --font-size 14 /tmp/demo.cast docs/demo.gif)
    (2.0, "j"),                      # profiles: move to demo (only profile? default absent in demo home)
    (0.8, "\r"),                     # open profile -> bucket list
    (1.6, "j"),                      # backups -> documents
    (0.8, "\r"),                     # enter bucket
    (1.8, "j"),                      # cursor: reports/ -> notes.md
    (1.0, "p"),                      # floating CONTENT preview of the file
    (2.6, "j"), (0.5, "j"), (0.5, "j"),  # scroll the preview
    (1.4, "\x1b"),                   # close preview
    (0.8, "m"),                      # floating METADATA overlay
    (2.8, "\x1b"),                   # close metadata
    (0.8, "l"),                      # dual-pane, local pane focused at cwd
    (1.6, "\r"),                     # enter project/ dir (dirs first)
    (1.2, " "),                      # select readme.md? (cursor on first entry)
    (0.6, " "),                      # select second
    (0.8, "u"),                      # upload selection -> confirm modal with buttons
    (1.8, "\r"),                     # enter = Yes (default)
    (2.6, "\t"),                     # tab to remote pane (uploaded files visible)
    (1.2, "y"),                      # yank s3:// URI (statusbar note)
    (1.6, "t"),                      # transfers overlay (100% bars)
    (2.4, "t"),                      # close
    (0.8, "?"),                      # help
    (1.6, "j"), (0.4, "j"), (0.4, "j"), (0.4, "j"), (0.4, "j"), (0.4, "j"),
    (1.2, "G"),                      # bottom
    (1.4, "\x1b"),                   # close help
    (1.0, "l"),                      # back to single pane
    (1.4, "q"),                      # quit
]

env = dict(os.environ)
env["HOME"] = "/tmp/demo-home"
env["XDG_CONFIG_HOME"] = "/tmp/demo-home/.config"
env["XDG_STATE_HOME"] = "/tmp/demo-home/.state"
env["TERM"] = "xterm-256color"
env["LANG"] = "en_US.UTF-8"

pid, fd = pty.fork()
if pid == 0:
    os.chdir("/tmp/demo-home/workspace")
    os.execve("/tmp/lazys3-demo", ["/tmp/lazys3-demo"], env)

# set window size
import fcntl, termios, struct
fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))

events = []
start = time.monotonic()
script = list(SCRIPT)
next_at = start + script[0][0] if script else None

def drain(timeout):
    while True:
        r, _, _ = select.select([fd], [], [], timeout)
        if not r:
            return True
        try:
            data = os.read(fd, 65536)
        except OSError:
            return False
        if not data:
            return False
        events.append([time.monotonic() - start, "o",
                       data.decode("utf-8", "replace")])
        timeout = 0.02  # keep slurping bursts

alive = True
while alive and script:
    now = time.monotonic()
    if now >= next_at:
        _, keys = script.pop(0)
        os.write(fd, keys.encode())
        if script:
            next_at = time.monotonic() + script[0][0]
    alive = drain(min(0.05, max(0.0, next_at - time.monotonic())) if script else 0.5)

# final drain until process exits (q was sent)
deadline = time.monotonic() + 5
while alive and time.monotonic() < deadline:
    alive = drain(0.2)

try:
    os.close(fd)
except OSError:
    pass
os.waitpid(pid, 0)

with open(CAST, "w") as f:
    f.write(json.dumps({"version": 2, "width": COLS, "height": ROWS,
                        "title": "lazys3 demo"}) + "\n")
    for ev in events:
        f.write(json.dumps(ev) + "\n")
print(f"wrote {CAST} with {len(events)} events, "
      f"duration {events[-1][0]:.1f}s" if events else "no events!")
