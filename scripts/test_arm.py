#!/usr/bin/env python3
"""Run release archives on emulated Pi CPUs. Requires qemu-user and Python 3."""
import hashlib
import datetime
import json
import os
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parents[1]
ARCHIVES = Path(sys.argv[1] if len(sys.argv) > 1 else ROOT / "dist").resolve()
checksums = {}
for line in (ARCHIVES / "checksums.txt").read_text().splitlines():
    digest, name = line.split()
    checksums[name] = digest

for arch, emulator, cpu in [
    ("armv6", os.environ.get("QEMU_ARM", "qemu-arm"), "arm1176"),
    ("armv7", os.environ.get("QEMU_ARM", "qemu-arm"), "cortex-a7"),
    ("arm64", os.environ.get("QEMU_AARCH64", "qemu-aarch64"), "cortex-a53"),
]:
    archive = ARCHIVES / f"orchard_linux_{arch}.tar.gz"
    assert hashlib.sha256(archive.read_bytes()).hexdigest() == checksums[archive.name]
    with tempfile.TemporaryDirectory(prefix=f"orchard-{arch}-") as td:
        with tarfile.open(archive) as tar:
            tar.extractall(td, filter="data")
        command = [emulator, "-cpu", cpu, str(Path(td) / "orchard")]
        print(f"Testing {arch} on {cpu}", flush=True)
        subprocess.run([*command, "--version"], check=True, timeout=30)
        fixture = Path(td) / "fixture"
        fixture.mkdir()
        (fixture / "nested").mkdir()
        (fixture / "nested" / "payload").write_bytes(b"x" * 8193)
        os.link(fixture / "nested" / "payload", fixture / "hardlink")
        os.symlink(fixture, fixture / "cycle")
        with (fixture / "sparse").open("wb") as sparse:
            sparse.truncate(1 << 30)
        report = json.loads(subprocess.check_output(
            [*command, "--scan", str(fixture)], text=True, timeout=30))
        stats = report["stats"]
        for child in report["children"]:
            datetime.datetime.fromisoformat(child["modified"])
            if child["created"] is not None:
                datetime.datetime.fromisoformat(child["created"])
        assert stats["Done"] and not stats["Cancelled"] and stats["Errors"] == 0, stats
        assert stats["Files"] == 4 and stats["Directories"] == 2 and stats["Hardlinks"] == 1, stats
        assert stats["Apparent"] > stats["Allocated"], stats
        subprocess.run([sys.executable, str(ROOT / "scripts/test_tui.py")], check=True,
                       env=dict(os.environ, ORCHARD_TEST_COMMAND=json.dumps(command)), timeout=90)
print("All ARM releases passed checksum, scan accounting, and interactive PTY tests.")
