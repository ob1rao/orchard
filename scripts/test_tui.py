#!/usr/bin/env python3
"""Real PTY smoke test, using only the Python standard library."""
import errno
import fcntl
import json
import os
from pathlib import Path
import pty
import select
import signal
import struct
import tempfile
import termios
import time

BINARY = Path(__file__).resolve().parents[1] / "bin" / "orchard"
COMMAND = json.loads(os.environ.get("ORCHARD_TEST_COMMAND", json.dumps([str(BINARY)])))

class Terminal:
    def __init__(self, *args):
        self.pid, self.fd = pty.fork()
        if self.pid == 0:
            fcntl.ioctl(0, termios.TIOCSWINSZ, struct.pack("HHHH", 32, 120, 0, 0))
            os.environ.update(TERM="xterm-256color", COLORTERM="truecolor")
            os.execvp(COMMAND[0], [*COMMAND, *map(str, args)])
        self.output = b""
        self.closed = False

    def read(self, seconds=0.4):
        until = time.monotonic() + seconds
        chunk = b""
        while time.monotonic() < until:
            ready, _, _ = select.select([self.fd], [], [], max(0, until - time.monotonic()))
            if not ready:
                break
            try:
                data = os.read(self.fd, 65536)
            except OSError as exc:
                if exc.errno == errno.EIO:
                    break
                raise
            if not data:
                break
            chunk += data
        self.output += chunk
        return chunk

    def expect(self, text, seconds=8):
        until = time.monotonic() + seconds
        while text.encode() not in self.output and time.monotonic() < until:
            self.read(0.2)
        assert text.encode() in self.output, f"missing {text!r}: {self.output[-3000:]!r}"

    def send(self, data):
        self.output = b""
        os.write(self.fd, data)
        self.read()

    def close(self):
        if self.closed:
            return
        self.send(b"q")
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            pid, status = os.waitpid(self.pid, os.WNOHANG)
            if pid:
                os.close(self.fd); self.closed = True
                assert os.waitstatus_to_exitcode(status) == 0, status
                assert b"\x1b[?1049l" in self.output, "alternate screen was not restored"
                return
            self.read(0.1)
        os.kill(self.pid, signal.SIGKILL); os.waitpid(self.pid, 0)
        os.close(self.fd); self.closed = True
        raise AssertionError("TUI failed to exit promptly")

with tempfile.TemporaryDirectory(prefix="orchard-pty-") as td:
    root = Path(td)
    (root / "payload").mkdir()
    (root / "payload" / "deep-file-unique.dat").write_bytes(b"x" * 100_000)
    (root / "small.txt").write_bytes(b"tiny")
    (root / ".secret").write_bytes(b"hidden")
    terminal = Terminal(root)
    try:
        terminal.expect("COMPLETE")
        terminal.expect("SPACE MAP")
        terminal.expect(".secret")
        terminal.expect("Hidden: on")
        terminal.send(b".\x0c")
        terminal.expect("Hidden: off")
        assert b".secret" not in terminal.output, "hidden file remains on screen"
        terminal.send(b".\x0c")
        terminal.expect(".secret")
        terminal.send(b"\r")
        terminal.expect("deep-file-unique.dat")
        terminal.send(b"\x7f")
        terminal.expect("small.txt")
        # SGR mouse: two clicks inside the largest tile, with release events.
        terminal.send(b"\x1b[<0;50;10M\x1b[<0;50;10m\x1b[<0;50;10M\x1b[<0;50;10m")
        terminal.expect("deep-file-unique.dat")
        terminal.send(b"/not-present\r")
        terminal.expect("No measurable matches")
        terminal.send(b"\x1b")
        terminal.read(0.5)
        fcntl.ioctl(terminal.fd, termios.TIOCSWINSZ, struct.pack("HHHH", 12, 40, 0, 0))
        os.kill(terminal.pid, signal.SIGWINCH)
        terminal.output = b""
        terminal.expect("resize terminal")
    finally:
        terminal.close()

terminal = Terminal()
try:
    terminal.expect("Select a disk")
    terminal.expect("Enter / click scan")
finally:
    terminal.close()
print("TUI: disk picker, rendering, keyboard, mouse, filtering, hidden toggle, resize, and terminal restoration passed")
