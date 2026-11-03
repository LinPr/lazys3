#!/usr/bin/env python3
"""Record lazys3 demo sessions from a pty and render GIF/PNG assets.

This replaces the old asciinema-cast script: that produced a .cast which
had to be converted with `agg`, and `agg` needs a full ffmpeg build that
slim images often lack. Rendering frames directly with Pillow means the
demo can be regenerated anywhere Python runs, and the same scenes double
as the still screenshots in the README.

Pipeline: spawn lazys3 in a pty -> feed a scripted key sequence -> pyte
parses the ANSI stream into a cell grid (colors included) -> Pillow paints
each distinct frame -> GIF.

Usage
-----
    # Terminal 1: seeded in-memory S3, no credentials needed
    go run ./cmd/demosrv

    # One-time demo HOME + binary
    ./docs/demo/build-demo-home.sh
    go build -o .cache/demo/lazys3 .

    # Assets
    python3 docs/demo/record.py gif  --scene tour   --out docs/demo/demo.gif
    python3 docs/demo/record.py shot --scene search --out docs/demo/search.png

Requires: pip install pyte pillow
"""
import argparse
import fcntl
import os
import pty
import select
import struct
import subprocess
import sys
import termios
import time

import pyte
from PIL import Image, ImageDraw, ImageFont

COLS, ROWS = 110, 32
# Render at 2x and box-filter down: anti-aliased text then has smooth,
# repeating gradients rather than per-pixel noise, which is what lets GIF's
# LZW actually compress the frames.
SS = 2

FONT_CANDIDATES = [
    "/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
    "/usr/share/fonts/TTF/DejaVuSansMono.ttf",
    "/System/Library/Fonts/Menlo.ttc",
    "C:/Windows/Fonts/consola.ttf",
]

# Base colours, i.e. what the *terminal* would use for default fg/bg. The
# renderer needs both variants because lazys3 itself does not paint a
# background: on a light terminal the theme's dark text sits on a light
# canvas, and drawing it on a dark canvas would make it invisible.
BG = (16, 18, 24)
FG_DEFAULT = (205, 214, 224)

LIGHT_BG = (247, 241, 232)
LIGHT_FG = (62, 50, 38)


def set_base_colors(light):
    global BG, FG_DEFAULT
    if light:
        BG, FG_DEFAULT = LIGHT_BG, LIGHT_FG
TITLE_BG = (30, 34, 44)
TITLE_FG = (150, 160, 175)

# xterm's default 16 colors; the TUI theme is built on top of these.
BOX_CHARS = set("─│┌┐└┘├┤┬┴┼━┃╭╮╰╯╴╵╶╷")

ANSI16 = [
    (0, 0, 0), (205, 49, 49), (13, 188, 121), (229, 229, 16),
    (36, 114, 200), (188, 63, 188), (17, 168, 205), (229, 229, 229),
    (102, 102, 102), (241, 76, 76), (35, 209, 139), (245, 245, 67),
    (59, 142, 234), (214, 112, 214), (41, 184, 219), (255, 255, 255),
]


def find_font(size):
    for path in FONT_CANDIDATES:
        if os.path.exists(path):
            return ImageFont.truetype(path, size)
    return ImageFont.load_default()


def color_of(name, default):
    """pyte reports colors as names, 256-color hex triples, or ints."""
    if name in (None, "default"):
        return default
    if name in pyte.graphics.FG_ANSI:
        return ANSI16[pyte.graphics.FG_ANSI[name]]
    if name in pyte.graphics.FG_AIXTERM:
        return ANSI16[pyte.graphics.FG_AIXTERM[name]]
    if isinstance(name, str) and len(name) == 6:
        try:
            return tuple(int(name[i:i + 2], 16) for i in (0, 2, 4))
        except ValueError:
            pass
    if isinstance(name, int):
        return ANSI16[name % len(ANSI16)]
    return default


class Renderer:
    """Paints a pyte Screen to a PIL image using the demo's dark palette."""

    def __init__(self, cols, rows, font_size=15, title="", supersample=1):
        self.cols, self.rows = cols, rows
        self.font_size = font_size
        self.ss = supersample
        self.font = find_font(font_size * supersample)
        self.title = title
        # Cell metrics come from the box-drawing glyphs, not from "M": a
        # renderer must make "─" span the full cell width and "│" the full
        # cell height, otherwise the pane borders look dashed even though
        # the TUI emitted a solid box. DejaVu's "─" is ~1.2x wider than "M"
        # and "│" is taller than the typical line height, so measuring "M"
        # (the obvious choice) produces the broken borders.
        self.cw = max(int(round(self._advance("─"))), 1)
        # Pad the line box so consecutive "│" glyphs touch.
        self.ch = max(int(round(self.font_size * 1.42)), self._ink_height("│") + 1)
        self.pad = 10 * supersample
        self.title_h = (26 if title else 0) * supersample

    def _advance(self, ch):
        if hasattr(self.font, "getlength"):
            return self.font.getlength(ch)
        return self.font.getsize(ch)[0]

    def _ink_height(self, ch):
        probe = Image.new("L", (self.cw * 2, self.font_size * 3), 0)
        ImageDraw.Draw(probe).text((0, 0), ch, font=self.font, fill=255)
        bbox = probe.getbbox()
        return (bbox[3] - bbox[1]) if bbox else self.font_size

    def downsample(self, img):
        """Box-filter the supersampled canvas to the requested scale."""
        if self.ss == 1:
            return img
        w, h = img.size
        return img.resize((w // self.ss, h // self.ss), Image.BOX)

    def canvas_size(self):
        return (self.cols * self.cw + 2 * self.pad,
                self.rows * self.ch + 2 * self.pad + self.title_h)

    def _draw_box(self, d, text, px, py, width, fg):
        """Paint box-drawing characters as strokes that fill the cell.

        Each corner must join exactly two arms: a top-left corner is
        right+down, a bottom-right is left+up. Drawing "a horizontal and a
        vertical" for every corner (an easy mistake) turns all four into
        little crosses with arms sticking out of the frame, which reads as
        a broken/dashed border.
        """
        thick = 1
        for idx, ch in enumerate(text):
            cx = px + idx * self.cw
            left, right = cx, cx + self.cw
            mid_x = cx + self.cw // 2
            mid_y = py + self.ch // 2

            def h(a, b):
                d.rectangle([a, mid_y, b, mid_y + thick], fill=fg)

            def v(a, b):
                d.rectangle([mid_x, a, mid_x + thick, b], fill=fg)

            # Straight runs.
            if ch in "─━":
                h(left, right)
            if ch in "│┃":
                v(py, py + self.ch)
            # Tees.
            if ch in "┬":
                h(left, right); v(py + self.ch // 2, py + self.ch)
            if ch in "┴":
                h(left, right); v(py, mid_y + thick)
            if ch in "├":
                v(py, py + self.ch); h(mid_x, right)
            if ch in "┤":
                v(py, py + self.ch); h(left, mid_x + thick)
            if ch == "┼":
                h(left, right); v(py, py + self.ch)
            # Corners: exactly two arms, pointing into the frame.
            if ch in "┌╭":
                h(mid_x, right); v(mid_y, py + self.ch)
            if ch in "┐╮":
                h(left, mid_x + thick); v(mid_y, py + self.ch)
            if ch in "└╰":
                h(mid_x, right); v(py, mid_y + thick)
            if ch in "┘╯":
                h(left, mid_x + thick); v(py, mid_y + thick)
            # Half-lines (used by rounded stems).
            if ch in "╴":
                h(left, mid_x + thick)
            if ch in "╶":
                h(mid_x, right)
            if ch in "╵":
                v(py, mid_y + thick)
            if ch in "╷":
                v(mid_y, py + self.ch)

    def render(self, screen):
        img = Image.new("RGB", self.canvas_size(), BG)
        d = ImageDraw.Draw(img)
        if self.title:
            d.rectangle([0, 0, self.canvas_size()[0], self.title_h], fill=TITLE_BG)
            d.text((self.pad, 6 * self.ss), self.title, font=self.font, fill=TITLE_FG)

        for y in range(self.rows):
            row = screen.buffer[y]
            x = 0
            while x < self.cols:
                cell = row[x]
                end = x + 1
                # Merge a run of cells sharing styling into one draw call.
                while end < self.cols:
                    nxt = row[end]
                    if (nxt.fg, nxt.bg, nxt.bold) != (cell.fg, cell.bg, cell.bold):
                        break
                    end += 1
                text = "".join(row[i].data for i in range(x, end))
                px = self.pad + x * self.cw
                py = self.pad + self.title_h + y * self.ch
                bg = color_of(cell.bg, BG)
                if bg != BG:
                    d.rectangle([px, py, px + (end - x) * self.cw, py + self.ch], fill=bg)
                if text.strip():
                    fg = color_of(cell.fg, FG_DEFAULT)
                    if all(ch in BOX_CHARS for ch in text):
                        # Terminals draw box-drawing characters as strokes
                        # that span the whole cell; font glyphs carry side
                        # bearings and a taller/shorter ink box, so drawing
                        # them leaves gaps that read as a dashed border.
                        # Paint the strokes directly instead.
                        self._draw_box(d, text, px, py, (end - x) * self.cw, fg)
                    else:
                        d.text((px, py), text, font=self.font, fill=fg,
                               stroke_width=1 if cell.bold else 0,
                               stroke_fill=fg if cell.bold else None)
                x = end
        return self.downsample(img)


def spawn(argv, home, cols, rows, cwd=None):
    master, slave = pty.openpty()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", rows, cols, 0, 0))
    env = dict(os.environ)
    # The recorder must own the terminal-capability variables: inheriting a
    # developer shell that sets NO_COLOR=1 or TERM=dumb makes lipgloss drop
    # every color, and the GIF/screenshots come out monochrome with no hint
    # as to why.
    env.update({"TERM": "xterm-256color", "LANG": "C.UTF-8",
                "LC_ALL": "C.UTF-8", "COLORTERM": "truecolor"})
    for drop in ("NO_COLOR", "CLICOLOR", "CLICOLOR_FORCE", "FORCE_COLOR"):
        env.pop(drop, None)
    if home:
        env["HOME"] = os.path.abspath(home)
        env.pop("XDG_CONFIG_HOME", None)
        env.pop("XDG_STATE_HOME", None)
    proc = subprocess.Popen(argv, stdin=slave, stdout=slave, stderr=slave,
                            env=env, close_fds=True, preexec_fn=os.setsid,
                            cwd=cwd)
    os.close(slave)
    return proc, master


def pump(master, stream, seconds, shot=None, interval=0.12):
    """Read pty output for `seconds`; `shot` collects periodic frames."""
    end = time.time() + seconds
    next_at = time.time()
    while time.time() < end:
        r, _, _ = select.select([master], [], [], 0.02)
        if r:
            try:
                data = os.read(master, 1 << 16)
            except OSError:
                return
            if not data:
                return
            stream.feed(data)
        if shot is not None and time.time() >= next_at:
            shot()
            next_at = time.time() + interval


def play(scene, argv, home, cwd=None):
    screen = pyte.Screen(COLS, ROWS)
    stream = pyte.ByteStream(screen)
    renderer = Renderer(COLS, ROWS, title=scene["title"], supersample=SS)
    frames = []

    def shot():
        frames.append(renderer.render(screen))

    proc, master = spawn(argv, home, COLS, ROWS, cwd=cwd)
    try:
        pump(master, stream, scene.get("startup", 2.5), shot)
        for delay, keys in scene["keys"]:
            if keys:
                os.write(master, keys.encode())
            pump(master, stream, delay, shot)
        pump(master, stream, scene.get("settle", 0.8), shot)
    finally:
        proc.terminate()
        try:
            proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            proc.kill()
        os.close(master)
    return frames, renderer, screen


def drop_blank_leading(frames, min_ink=0.02):
    """Drop frames captured before the app has painted anything.

    The recorder samples from the moment the process spawns, so a GIF would
    otherwise open on a blank or half-drawn screen. "Ink" is the share of
    pixels that differ from the dominant background colour; a painted TUI
    (borders + text) clears a couple of percent, an empty canvas does not.
    """
    for i, f in enumerate(frames):
        small = f.convert("RGB").resize((80, 40), Image.BOX)
        pixels = list(small.getdata())
        bg = max(set(pixels), key=pixels.count)
        ink = sum(1 for p in pixels if p != bg) / len(pixels)
        if ink >= min_ink:
            return frames[i:]
    return frames


def dedupe(frames):
    out = [frames[0]]
    for f in frames[1:]:
        if f.tobytes() != out[-1].tobytes():
            out.append(f)
    return out


# Scene scripts. Keys are raw text for the pty: "\x1b" is esc, "\r" enter.
SCENES = {
    # The hero GIF: the four things a newcomer needs to see — browsing,
    # previewing without downloading, searching the bucket, and dual-pane.
    "tour": {
        "title": "lazys3 — browse · preview · search · dual-pane",
        "startup": 3.0,
        "args": "--profile demo",
        "keys": [
            (0.8, "enter" and "\r"),   # open the notes/ prefix
            (1.6, "\x7f"),            # back out
            (1.2, "j"), (0.4, "j"), (0.4, "j"),  # onto README.md
            (1.6, "p"),                # content preview
            (2.2, "\x1b"),             # close
            (1.2, "F"), (0.9, "report"), (0.6, "\r"),  # recursive search
            (2.4, "\x7f"),             # back up to the parent prefix
            (1.2, "l"),                # dual-pane local ⇄ remote
            (2.2, "\t"),               # switch focus to the local pane
            (2.0, "\x1b"),
        ],
    },
    "start": {
        "title": "lazys3 — per-profile start location (skips ListBuckets)",
        "startup": 3.5,
        "args": "--profile demo",
        "keys": [(2.0, "j"), (0.8, "j")],
    },
    "search": {
        "title": "lazys3 — recursive search with 'F'",
        "startup": 3.5,
        "args": "--profile demo",
        "keys": [(1.2, "F"), (1.0, "report"), (0.8, "\r"), (3.0, "")],
    },
    "transfers": {
        "args": "--profile demo",
        "title": "lazys3 — live transfers overlay ('t')",
        "startup": 3.5,
        "keys": [(1.2, "t"), (3.0, "")],
    },
    "help": {
        "args": "--profile demo",
        "title": "lazys3 — keybindings overlay ('?')",
        "startup": 3.5,
        "keys": [(1.2, "?"), (2.0, "j"), (1.0, "j")],
    },
    "metadata": {
        "args": "--profile demo",
        "title": "lazys3 — metadata overlay ('m')",
        "startup": 3.5,
        "keys": [(0.9, "j"), (0.4, "j"), (0.4, "j"), (1.2, "m"), (2.5, "")],
    },
    "preview": {
        "args": "--profile demo",
        "title": "lazys3 — content preview ('p')",
        "startup": 3.5,
        "keys": [(0.9, "j"), (0.4, "j"), (0.4, "j"), (1.2, "p"), (2.5, "")],
    },
    "dual": {
        "args": "--profile demo",
        "title": "lazys3 — dual-pane local ⇄ remote ('l')",
        "startup": 3.5,
        "keys": [(1.4, "l"), (2.5, "\t")],
    },
}


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("mode", choices=["gif", "shot"])
    ap.add_argument("--out", required=True)
    ap.add_argument("--scene", default="tour", choices=sorted(SCENES))
    ap.add_argument("--bin", default=os.environ.get("LAZYS3_BIN", ".cache/demo/lazys3"))
    ap.add_argument("--home", default=os.environ.get("LAZYS3_DEMO_HOME", ".cache/demo/home"))
    ap.add_argument("--config", default=os.environ.get("LAZYS3_DEMO_CONFIG", ""))
    ap.add_argument("--cwd", default=os.environ.get("LAZYS3_DEMO_CWD", ""))
    ap.add_argument("--extra", default="", help="extra lazys3 args")
    ap.add_argument("--light", action="store_true",
                    help="render for a light terminal (light fg/bg defaults)")
    ap.add_argument("--scale", type=float, default=0.72, help="GIF downscale factor")
    ap.add_argument("--colors", type=int, default=48, help="GIF palette size")
    ap.add_argument("--duration", type=int, default=110, help="ms per frame")
    args = ap.parse_args()

    set_base_colors(args.light)

    scene = SCENES[args.scene]
    argv = [os.path.abspath(args.bin)]
    if args.config:
        argv += ["--config", os.path.abspath(args.config)]
    argv += [a for a in scene.get("args", "").split() if a]
    argv += [a for a in args.extra.split() if a]

    frames, renderer, screen = play(
        scene, argv, args.home or None, cwd=args.cwd or None)

    out = os.path.abspath(args.out)
    os.makedirs(os.path.dirname(out) or ".", exist_ok=True)
    if args.mode == "gif":
        if not frames:
            sys.exit("no frames captured")
        frames = drop_blank_leading(frames)
        frames = dedupe(frames)
        # README GIFs must stay small: GitHub renders them inline, and a
        # multi-megabyte first paint is a bad first impression. Halving the
        # raster and quantizing to a small palette cuts the size by ~10x
        # while staying readable.
        scale = args.scale
        if scale != 1.0:
            frames = [f.resize((int(f.width * scale), int(f.height * scale)),
                               Image.LANCZOS) for f in frames]
        # Quantize explicitly with dithering OFF before saving. Passing
        # `colors=` to save() makes Pillow dither, which sprays near-equal
        # pixels across flat UI areas and roughly quadruples the GIF: LZW
        # cannot find runs in the resulting noise.
        pal = [f.quantize(colors=args.colors, method=Image.MEDIANCUT,
                          dither=Image.NONE) for f in frames]
        pal[0].save(out, save_all=True, append_images=pal[1:],
                    duration=args.duration, loop=0, optimize=True,
                    disposal=1)
        print(f"wrote {out}  ({len(frames)} frames, {os.path.getsize(out)//1024} KiB)")
    else:
        renderer.render(screen).save(out)
        print(f"wrote {out}")


if __name__ == "__main__":
    main()
