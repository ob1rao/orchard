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

def check(system, machine, arch, private=False, corrupt=False, bits="64", shell="bash", login=".profile"):
    with tempfile.TemporaryDirectory(prefix="orchard install test ") as td:
        root = Path(td).resolve()
        mocks, fixtures, dest = root / "mocks", root / "fixtures", root / "install dir's $literal `name`"
        mocks.mkdir(); fixtures.mkdir(); dest.mkdir()
        home = root / "home"
        home.mkdir()
        (home / login).write_text("# Existing configuration\n")
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
                   HOME=str(home), SHELL=f"/bin/{shell}", ZDOTDIR=str(home),
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
            if system == "Linux":
                profiles = [home / login, home / ".bashrc"] if shell == "bash" else [home / (".zshrc" if shell == "zsh" else ".profile")]
                before = [p.read_text() for p in profiles]
                repeat = subprocess.run(["sh", str(ROOT / "install.sh")], env=env, capture_output=True, text=True)
                assert repeat.returncode == 0, repeat.stderr
                assert before == [p.read_text() for p in profiles], "duplicate PATH entries on reinstall"
                for profile in profiles:
                    probe = subprocess.run(["sh", "-c", '. "$1"; . "$1"; command -v orchard; printf "%s\\n" "$PATH"', "sh", str(profile)], env=env, capture_output=True, text=True)
                    assert probe.returncode == 0, probe.stderr
                    lines = probe.stdout.splitlines()
                    assert lines[0] == str(dest / "orchard"), probe.stdout
                    assert lines[1].split(":").count(str(dest)) == 1, probe.stdout
            else:
                assert (home / login).read_text() == "# Existing configuration\n"
                assert not (home / ".bashrc").exists()

for system, machine, arch in [("Linux", "x86_64", "amd64"), ("Linux", "aarch64", "arm64"),
                             ("Linux", "armv6l", "armv6"), ("Linux", "armv7l", "armv7"),
                             ("Darwin", "arm64", "arm64"), ("Darwin", "x86_64", "amd64")]:
    check(system, machine, arch)
check("Linux", "aarch64", "armv7", bits="32")
check("Linux", "armv8l", "armv7", bits="32")
check("Linux", "arm64", "arm64")
check("Linux", "x86_64", "amd64", private=True)
check("Linux", "x86_64", "amd64", corrupt=True)
for login in (".bash_profile", ".bash_login"):
    check("Linux", "aarch64", "arm64", login=login)
for shell in ("sh", "zsh"):
    check("Linux", "armv7l", "armv7", shell=shell)
print("Installer: all 15 platform/authentication/checksum/PATH cases passed")
