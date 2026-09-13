#!/usr/bin/env python3
"""Exercise the real installer with local release fixtures; never uses the network."""
import hashlib
import io
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parents[1]

def check(system, machine, arch, private=False, corrupt=False, bits="64"):
    with tempfile.TemporaryDirectory(prefix="orchard install test ") as td:
        root = Path(td)
        mocks, fixtures, dest = root / "mocks", root / "fixtures", root / "install dir"
        mocks.mkdir(); fixtures.mkdir(); dest.mkdir()
        name = f"orchard_{system.lower() if system == 'Linux' else 'darwin'}_{arch}.tar.gz"
        archive = fixtures / name
        payload = b"#!/bin/sh\nprintf 'orchard fixture\\n'\n"
        with tarfile.open(archive, "w:gz") as tar:
            info = tarfile.TarInfo("orchard"); info.size = len(payload); info.mode = 0o755
            tar.addfile(info, io.BytesIO(payload))
        digest = "0" * 64 if corrupt else hashlib.sha256(archive.read_bytes()).hexdigest()
        (fixtures / "checksums.txt").write_text(f"{digest}  {name}\n")
        (dest / "orchard").write_text("previous binary")
        scripts = {
            "uname": f'#!/bin/sh\ncase "$1" in -s) echo {system};; -m) echo {machine};; esac\n',
            "getconf": f"#!/bin/sh\necho {bits}\n",
            "curl": '''#!/bin/sh
url=''; dest=''
while [ "$#" -gt 0 ]; do
 case "$1" in https:*) url=$1;; -o) shift; dest=$1;; esac
 shift
done
cp "$FIXTURES/${url##*/}" "$dest"
''',
            "gh": '''#!/bin/sh
[ "$PRIVATE" = yes ] || exit 1
if [ "$1" = auth ]; then exit 0; fi
dest=''
while [ "$#" -gt 0 ]; do
 if [ "$1" = --dir ]; then shift; dest=$1; fi
 shift
done
cp "$FIXTURES/"* "$dest/"
''',
        }
        for name_, content in scripts.items():
            p = mocks / name_; p.write_text(content); p.chmod(0o755)
        env = dict(os.environ, PATH=f"{mocks}:/usr/bin:/bin", FIXTURES=str(fixtures),
                   PRIVATE="yes" if private else "no", ORCHARD_INSTALL_DIR=str(dest),
                   ORCHARD_VERSION="v0.1.0", ORCHARD_REPO="ob1rao/orchard")
        result = subprocess.run(["sh", str(ROOT / "install.sh")], env=env, capture_output=True, text=True)
        if corrupt:
            assert result.returncode != 0, result.stdout
            assert (dest / "orchard").read_text() == "previous binary", "failed install replaced existing binary"
        else:
            assert result.returncode == 0, result.stdout + result.stderr
            assert os.access(dest / "orchard", os.X_OK)
            assert (dest / "orchard").read_bytes() == payload

for system, machine, arch in [("Linux", "x86_64", "amd64"), ("Linux", "aarch64", "arm64"),
                             ("Linux", "armv6l", "armv6"), ("Linux", "armv7l", "armv7"),
                             ("Darwin", "arm64", "arm64"), ("Darwin", "x86_64", "amd64")]:
    check(system, machine, arch)
check("Linux", "aarch64", "armv7", bits="32")
check("Linux", "x86_64", "amd64", private=True)
check("Linux", "x86_64", "amd64", corrupt=True)
print("Installer: all 9 platform/authentication/checksum cases passed")
