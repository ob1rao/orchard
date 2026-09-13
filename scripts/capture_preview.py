#!/usr/bin/env python3
"""Capture Orchard's real PTY output using sparse demo files.

Development-only dependencies: Pillow, pyte; build bin/orchard first.
"""
import codecs
import fcntl
import os
from pathlib import Path
import pty
import select
import signal
import struct
import subprocess
import tempfile
import termios
import time

import pyte
from PIL import Image, ImageDraw, ImageFont

ROOT = Path(__file__).resolve().parents[1]
COLS, ROWS = 120, 32

with tempfile.TemporaryDirectory(prefix="orchard-demo-") as td:
    demo = Path(td)
    files = {
        "Videos/documentary.mp4": 38_000_000_000,
        "Videos/recordings.mp4": 24_000_000_000,
        "Backups/laptop.tar": 28_000_000_000,
        "Photos/library.zip": 19_500_000_000,
        "Projects/datasets.bin": 13_200_000_000,
        "Downloads/linux.iso": 6_400_000_000,
        ".cache/build-cache": 3_100_000_000,
        "notes.md": 84_000,
    }
    for name, size in files.items():
        path = demo / name
        path.parent.mkdir(exist_ok=True)
        with path.open("wb") as f:
            f.truncate(size)  # Sparse files: no large allocation or download.

    pid, fd = pty.fork()
    if pid == 0:
        fcntl.ioctl(0, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
        os.environ.update(TERM="xterm-256color", COLORTERM="truecolor")
        os.environ.pop("NO_COLOR", None)
        os.environ.pop("TCELL_TRUECOLOR", None)
        binary = ROOT / "bin/orchard"
        os.execv(str(binary), [str(binary), "--apparent", str(demo)])

    class CaptureScreen(pyte.Screen):
        def select_graphic_rendition(self, *attrs, **kwargs):
            # Ignore private SGR queries unsupported by older pyte versions.
            if not kwargs.get("private"):
                super().select_graphic_rendition(*attrs)

    screen = CaptureScreen(COLS, ROWS)
    stream = pyte.Stream(screen)
    decoder = codecs.getincrementaldecoder("utf-8")("replace")
    try:
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            ready, _, _ = select.select([fd], [], [], 0.2)
            if ready:
                stream.feed(decoder.decode(os.read(fd, 65536)))
            if not ready and any("COMPLETE" in row for row in screen.display):
                break
        else:
            raise RuntimeError("Orchard did not render a completed scan")
        # The capture is drawn directly from the terminal emulator's cell buffer.
        font_path = subprocess.check_output(
            ["fc-match", "-f", "%{file}", "DejaVu Sans Mono"], text=True)
        font = ImageFont.truetype(font_path, 20)
        cell_w, cell_h, padding = 12, 24, 16
        canvas = Image.new("RGB", (COLS * cell_w + padding * 2,
                                   ROWS * cell_h + padding * 2), "#0d1420")
        draw = ImageDraw.Draw(canvas)
        colors = {"default": "dce7f4", "black": "000000", "red": "cc3333",
                  "green": "33cc66", "brown": "ccaa33", "blue": "4477cc",
                  "magenta": "bb55bb", "cyan": "55bbbb", "white": "dddddd"}
        def color(value, fallback):
            if value == "default":
                return fallback
            return "#" + colors.get(value, value)
        for y in range(ROWS):
            for x in range(COLS):
                cell = screen.buffer[y][x]
                fg, bg = color(cell.fg, "#dce7f4"), color(cell.bg, "#0d1420")
                if cell.reverse:
                    fg, bg = bg, fg
                px, py = padding + x * cell_w, padding + y * cell_h
                draw.rectangle((px, py, px + cell_w - 1, py + cell_h - 1), fill=bg)
                if cell.data.strip():
                    draw.text((px, py), cell.data, font=font, fill=fg)
        target = ROOT / "docs/assets/orchard-tui.png"
        target.parent.mkdir(parents=True, exist_ok=True)
        canvas.save(target, optimize=True)
        print(target)
    finally:
        os.write(fd, b"\x03")
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            if os.waitpid(pid, os.WNOHANG)[0]:
                break
            time.sleep(0.05)
        else:
            os.kill(pid, signal.SIGKILL)
            os.waitpid(pid, 0)
        os.close(fd)
