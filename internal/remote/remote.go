// Package remote runs Orchard scans on other machines over SSH.
//
// It drives the system ssh client rather than a Go implementation so that
// ~/.ssh/config, agents, jump hosts, hardware keys and Tailscale SSH all work
// exactly as they do from the shell. The remote side is Orchard itself, run
// headless: it scans and streams a snapshot back on stdout, with progress
// lines on stderr. When the remote machine has no Orchard that speaks this
// protocol and shares this machine's OS and architecture, Orchard uploads its
// own executable into ~/.cache/orchard on the remote machine first.
package remote

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/ob1rao/orchard/internal/snapshot"
)

// Protocol is the version of the headless exchange. A remote Orchard
// announces it with --remote-protocol.
const Protocol = 1

// Banner is what --remote-protocol prints.
var Banner = "orchard-remote " + strconv.Itoa(Protocol)

// ProgressPrefix starts each progress line a remote scan writes to stderr.
const ProgressPrefix = "ORCHARD-PROGRESS "

// Progress is a running scan's counters.
type Progress struct {
	Files       uint64 `json:"files"`
	Directories uint64 `json:"directories"`
	Bytes       uint64 `json:"bytes"`
	Errors      uint64 `json:"errors"`
}

// Target says how to reach a machine.
type Target struct {
	Host     string
	Port     int
	Identity string
	Options  []string
	// Binary is an orchard executable already on the remote machine.
	Binary string
	// Upload is a local executable to install remotely in place of this
	// one, such as a cross-compiled release for another architecture.
	Upload string
	Sudo   bool
	// Batch forbids ssh from prompting, which a full-screen interface needs:
	// a password prompt would draw over it and never be seen.
	Batch bool
	// Stage receives short descriptions of what is happening.
	Stage func(string)
}

func (t Target) stage(s string) {
	if t.Stage != nil {
		t.Stage(s)
	}
}

// SSH is the client binary; tests replace it.
var SSH = "ssh"

func (t Target) command(ctx context.Context, script string) *exec.Cmd {
	args := []string{"-o", "ConnectTimeout=15"}
	if t.Batch {
		args = append(args, "-o", "BatchMode=yes")
	}
	if t.Port != 0 {
		args = append(args, "-p", strconv.Itoa(t.Port))
	}
	if t.Identity != "" {
		args = append(args, "-i", t.Identity)
	}
	for _, o := range t.Options {
		args = append(args, "-o", o)
	}
	// The remote login shell might be fish or csh; hand the script to sh so
	// it means the same thing everywhere.
	args = append(args, "--", t.Host, "sh -c "+Quote(script))
	cmd := exec.CommandContext(ctx, SSH, args...)
	return cmd
}

// Quote makes s a single POSIX shell word.
func Quote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func executable() string {
	p, err := os.Executable()
	if err != nil {
		return os.Args[0]
	}
	return p
}

func fileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil))[:12], nil
}

// probe reports the remote platform and a usable orchard, if any.
type probe struct {
	OS, Arch, Binary string
}

func (t Target) probe(ctx context.Context, cached string) (probe, error) {
	candidates := []string{}
	if t.Binary != "" {
		candidates = append(candidates, Quote(t.Binary))
	}
	candidates = append(candidates, `"$HOME/.local/bin/orchard"`, `"$(command -v orchard 2>/dev/null)"`, `"$HOME/.cache/orchard/`+cached+`"`)
	script := `uname -s; uname -m
for c in ` + strings.Join(candidates, " ") + `; do
  if [ -n "$c" ] && [ -x "$c" ] && "$c" --remote-protocol 2>/dev/null | grep -qx ` + Quote(Banner) + `; then echo "$c"; exit 0; fi
done
echo`
	out, err := t.run(ctx, script, nil)
	if err != nil {
		return probe{}, err
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) < 2 {
		return probe{}, fmt.Errorf("unexpected reply from %s: %q", t.Host, out)
	}
	p := probe{OS: goos(lines[0]), Arch: goarch(lines[1])}
	if len(lines) > 2 {
		p.Binary = strings.TrimSpace(lines[2])
	}
	return p, nil
}

func goos(uname string) string { return strings.ToLower(strings.TrimSpace(uname)) }
func goarch(m string) string {
	switch m = strings.TrimSpace(m); m {
	case "x86_64", "amd64":
		return "amd64"
	case "aarch64", "arm64":
		return "arm64"
	case "i386", "i686":
		return "386"
	}
	if strings.HasPrefix(m, "armv") {
		return "arm"
	}
	return m
}

// run executes a script and returns stdout, folding stderr into the error.
func (t Target) run(ctx context.Context, script string, stdin io.Reader) ([]byte, error) {
	cmd := t.command(ctx, script)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr, cmd.Stdin = &stdout, &stderr, stdin
	if stdin == nil && !t.Batch {
		// Leave the terminal to ssh so it can ask for a password or to
		// trust a new host key.
		cmd.Stdin = os.Stdin
	}
	if err := cmd.Run(); err != nil {
		return nil, sshError(t.Host, err, stderr.String())
	}
	return stdout.Bytes(), nil
}

func sshError(host string, err error, stderr string) error {
	msg := strings.TrimSpace(stderr)
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 255 {
		if msg == "" {
			msg = "connection failed"
		}
		if strings.Contains(msg, "Permission denied") || strings.Contains(msg, "Host key verification failed") {
			msg += " (run `ssh " + host + "` once to trust the host and set up keys)"
		}
		return fmt.Errorf("ssh %s: %s", host, lastLines(msg, 3))
	}
	if msg != "" {
		return fmt.Errorf("%s: %s", host, lastLines(msg, 4))
	}
	return fmt.Errorf("%s: %w", host, err)
}

func lastLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "; ")
}

// Prepare finds or installs an orchard on the target and returns its path.
func (t Target) Prepare(ctx context.Context) (string, error) {
	upload := t.Upload
	if upload == "" {
		upload = executable()
	}
	hash, err := fileHash(upload)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", upload, err)
	}
	cached := "orchard-" + hash
	t.stage("connecting to " + t.Host)
	p, err := t.probe(ctx, cached)
	if err != nil {
		return "", err
	}
	if p.Binary != "" {
		return p.Binary, nil
	}
	if t.Upload == "" && (p.OS != runtime.GOOS || p.Arch != runtime.GOARCH) {
		return "", fmt.Errorf("%s is %s/%s and has no orchard with remote support; this orchard is %s/%s. Install orchard there, or pass --upload with a %s/%s build",
			t.Host, p.OS, p.Arch, runtime.GOOS, runtime.GOARCH, p.OS, p.Arch)
	}
	t.stage(fmt.Sprintf("installing orchard on %s (%s/%s)", t.Host, p.OS, p.Arch))
	f, err := os.Open(upload)
	if err != nil {
		return "", err
	}
	defer f.Close()
	dest := "$HOME/.cache/orchard/" + cached
	script := `set -e; mkdir -p "$HOME/.cache/orchard"; umask 077; cat > "` + dest + `.part"; chmod 700 "` + dest + `.part"; mv "` + dest + `.part" "` + dest + `"; ` +
		`"` + dest + `" --remote-protocol >/dev/null; echo "` + dest + `"`
	out, err := t.run(ctx, script, f)
	if err != nil {
		// A binary that uploads but will not start is almost always linked
		// against a C library the remote machine lacks, such as glibc on
		// Alpine's musl. Release builds are static; say so.
		return "", fmt.Errorf("installing orchard on %s: %w (the uploaded build could not start there; use a static build: CGO_ENABLED=0 go build, or a release binary via --upload)", t.Host, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Scan scans path on the target and returns the snapshot it sends back,
// labelled with machine as its machine name.
func (t Target) Scan(ctx context.Context, machine, path string, progress func(Progress)) (*snapshot.Snapshot, error) {
	bin, err := t.Prepare(ctx)
	if err != nil {
		return nil, err
	}
	t.stage("scanning " + path + " on " + t.Host)
	script := "exec "
	if t.Sudo {
		script += "sudo -n "
	}
	script += Quote(bin) + " --scan --emit-snapshot --progress -- " + Quote(path)
	cmd := t.command(ctx, script)
	if !t.Batch {
		cmd.Stdin = os.Stdin
	}
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	var tail []string
	sc := bufio.NewScanner(stderr)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if rest, ok := strings.CutPrefix(line, ProgressPrefix); ok {
			var p Progress
			if json.Unmarshal([]byte(rest), &p) == nil && progress != nil {
				progress(p)
			}
			continue
		}
		tail = append(tail, line)
		if len(tail) > 20 {
			tail = tail[1:]
		}
	}
	if err := cmd.Wait(); err != nil {
		return nil, sshError(t.Host, err, strings.Join(tail, "\n"))
	}
	snap, err := snapshot.Decode(stdout.Bytes())
	if err != nil {
		return nil, fmt.Errorf("%s sent an unreadable snapshot: %w", t.Host, err)
	}
	snap.Meta.ID = snapshot.NewID()
	snap.Meta.Machine = machine
	snap.Meta.Source = "ssh:" + t.Host
	return snap, nil
}
