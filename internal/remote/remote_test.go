package remote

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeSSH stands in for ssh: it drops ssh's own arguments and runs the
// remote command locally, with HOME pointing at a scratch "remote" home.
const fakeSSH = `#!/bin/sh
while [ "$1" != "--" ]; do shift; done
shift 2
exec /bin/sh -c "$1"
`

func TestQuote(t *testing.T) {
	got, err := exec.Command("/bin/sh", "-c", "printf %s "+Quote(`it's "$HOME" \n`)).Output()
	if err != nil || string(got) != `it's "$HOME" \n` {
		t.Fatalf("quoted word came back as %q (%v)", got, err)
	}
}

func TestArchitectureNames(t *testing.T) {
	for in, want := range map[string]string{"x86_64": "amd64", "aarch64": "arm64", "armv7l": "arm", "armv6l": "arm", "riscv64": "riscv64"} {
		if got := goarch(in); got != want {
			t.Errorf("goarch(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestScanOverFakeSSH runs the whole exchange against a real Orchard build:
// probe, upload, a headless scan streaming a snapshot back, and reuse of
// the uploaded copy the second time.
func TestScanOverFakeSSH(t *testing.T) {
	if testing.Short() {
		t.Skip("builds orchard")
	}
	goTool, err := exec.LookPath("go")
	if err != nil {
		goTool = filepath.Join(runtime.GOROOT(), "bin", "go")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "orchard")
	build := exec.Command(goTool, "build", "-o", bin, "github.com/ob1rao/orchard/cmd/orchard")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Skipf("cannot build orchard: %v\n%s", err, out)
	}
	ssh := filepath.Join(dir, "ssh")
	if err := os.WriteFile(ssh, []byte(fakeSSH), 0o700); err != nil {
		t.Fatal(err)
	}
	old := SSH
	SSH = ssh
	defer func() { SSH = old }()
	home := filepath.Join(dir, "remote-home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/usr/bin:/bin") // no orchard on the "remote" PATH

	target := filepath.Join(dir, "target")
	if err := os.MkdirAll(filepath.Join(target, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "sub", "file"), make([]byte, 12345), 0o600); err != nil {
		t.Fatal(err)
	}

	var stages []string
	var progressed bool
	tg := Target{Host: "fake", Batch: true, Upload: bin, Stage: func(s string) { stages = append(stages, s) }}
	snap, err := tg.Scan(context.Background(), "web1", target, func(Progress) { progressed = true })
	if err != nil {
		t.Fatal(err)
	}
	if snap.Meta.Machine != "web1" || snap.Meta.Source != "ssh:fake" || snap.Meta.Stats.Apparent < 12345 {
		t.Fatalf("snapshot meta: %+v", snap.Meta)
	}
	if !progressed {
		t.Error("no progress lines arrived")
	}
	if !strings.Contains(strings.Join(stages, "|"), "installing") {
		t.Errorf("expected an upload, got stages %v", stages)
	}
	cached, _ := filepath.Glob(filepath.Join(home, ".cache", "orchard", "orchard-*"))
	if len(cached) != 1 {
		t.Fatalf("uploaded copies: %v", cached)
	}

	stages = nil
	if _, err := tg.Scan(context.Background(), "web1", target, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(stages, "|"), "installing") {
		t.Errorf("uploaded again instead of reusing the cached copy: %v", stages)
	}

	if _, err := tg.Scan(context.Background(), "web1", filepath.Join(dir, "missing"), nil); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Errorf("scan of a missing directory should fail and say why, got %v", err)
	}
}
