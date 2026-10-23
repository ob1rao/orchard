package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ob1rao/orchard/internal/diff"
	"github.com/ob1rao/orchard/internal/library"
	"github.com/ob1rao/orchard/internal/query"
	"github.com/ob1rao/orchard/internal/remote"
	"github.com/ob1rao/orchard/internal/scan"
	"github.com/ob1rao/orchard/internal/snapshot"
	"github.com/ob1rao/orchard/internal/ui"
	"golang.org/x/term"
)

type command struct {
	usage, summary string
	run            func(ctx context.Context, args []string) int
}

var commands map[string]command

// Commands claim the first argument, so a directory that shares a command's
// name must be written as a path: orchard ./save.
func init() {
	commands = map[string]command{
		"save":      {"save [--label TEXT] [directory]", "scan without the TUI and save a snapshot (default: current directory)", cmdSave},
		"snapshots": {"snapshots [--machine NAME] [--json]", "list saved snapshots", cmdSnapshots},
		"ls":        {"", "", cmdSnapshots},
		"open":      {"open REF", "browse a saved snapshot offline", cmdOpen},
		"show":      {"show [--json] REF", "print a snapshot's summary", cmdShow},
		"diff":      {"diff [--text|--json] [--top N] BEFORE AFTER", "compare two snapshots (TUI on a terminal)", cmdDiff},
		"delete":    {"delete REF...", "remove snapshots from the library", cmdDelete},
		"archive":   {"archive export|import|list ...", "move snapshots to and from a portable archive file", cmdArchive},
		"machine":   {"machine add|list|show|remove ...", "manage saved machine profiles", cmdMachine},
		"remote":    {"remote [--no-open] [--upload FILE] MACHINE|[user@]host [directory]", "scan another machine over SSH and save the snapshot", cmdRemote},
		"query":     {"query [--all] [--json] [--limit N] [--group machine|ext] EXPR", "search saved snapshots across machines", cmdQuery},
		"library":   {"library", "open the TUI on the snapshot library", cmdLibrary},
		"fleet":     {"fleet", "open the TUI on saved machines", cmdFleet},
	}
}

func printCommands(w io.Writer) {
	names := make([]string, 0, len(commands))
	for n, c := range commands {
		if c.usage != "" {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	tw := tabwriter.NewWriter(w, 2, 4, 2, ' ', 0)
	for _, n := range names {
		fmt.Fprintf(tw, "  orchard %s\t%s\n", commands[n].usage, commands[n].summary)
	}
	tw.Flush()
	fmt.Fprintln(w, "\nA snapshot REF is an ID prefix, a machine name (its newest snapshot), MACHINE~N")
	fmt.Fprintln(w, "for the Nth snapshot before that, or a path to a .orchard file.")
	fmt.Fprintln(w, "Data lives in $ORCHARD_HOME, else $XDG_DATA_HOME/orchard, else ~/.local/share/orchard.")
}

func fail(err error) int {
	fmt.Fprintln(os.Stderr, "orchard:", err)
	return 1
}

func newFlags(name string) *flag.FlagSet {
	f := flag.NewFlagSet("orchard "+name, flag.ContinueOnError)
	f.Usage = func() {
		fmt.Fprintf(f.Output(), "Usage: orchard %s\n", commands[name].usage)
		f.PrintDefaults()
	}
	return f
}

// parse accepts flags after positional arguments too, which flag alone does
// not, so "orchard diff a b --text" works as people type it.
func parse(f *flag.FlagSet, args []string) ([]string, bool) {
	var positional []string
	for {
		if err := f.Parse(args); err != nil {
			return nil, false
		}
		args = f.Args()
		if len(args) == 0 {
			return positional, true
		}
		if args[0] == "--" {
			return append(positional, args[1:]...), true
		}
		positional = append(positional, args[0])
		args = args[1:]
	}
}

func openLibrary() (*library.Library, error) { return library.Open() }

func interactive() bool {
	return term.IsTerminal(int(os.Stdout.Fd())) && term.IsTerminal(int(os.Stdin.Fd()))
}

func saveTree(t *scan.Tree, started time.Time, label string, w io.Writer) int {
	lib, err := openLibrary()
	if err != nil {
		return fail(err)
	}
	e, err := lib.Save(describe(t, started, label), t)
	if err != nil {
		return fail(err)
	}
	fmt.Fprintf(w, "saved snapshot %s · %s · %s · %s\n", e.ID, e.Machine, e.Root, ui.Bytes(e.Stats.Allocated))
	return 0
}

func cmdSave(ctx context.Context, args []string) int {
	f := newFlags("save")
	label := f.String("label", "", "a note to keep with the snapshot")
	workers := f.Int("workers", 0, "directory workers (0 tunes the count to the disk)")
	pos, ok := parse(f, args)
	if !ok || len(pos) > 1 {
		return 2
	}
	path := "."
	if len(pos) == 1 {
		path = pos[0]
	}
	var report func(scan.Stats)
	if term.IsTerminal(int(os.Stderr.Fd())) {
		report = func(s scan.Stats) {
			fmt.Fprintf(os.Stderr, "\rscanning %s · %d files · %d dirs · %s ", path, s.Files, s.Directories, ui.Bytes(s.Allocated))
		}
	}
	t, started, err := scanToDone(ctx, path, scan.Options{Workers: *workers}, report)
	if err != nil {
		return fail(err)
	}
	if report != nil {
		fmt.Fprintln(os.Stderr)
	}
	return saveTree(t, started, *label, os.Stdout)
}

func cmdSnapshots(_ context.Context, args []string) int {
	f := newFlags("snapshots")
	machine := f.String("machine", "", "only this machine")
	asJSON := f.Bool("json", false, "print JSON")
	if _, ok := parse(f, args); !ok {
		return 2
	}
	lib, err := openLibrary()
	if err != nil {
		return fail(err)
	}
	all, err := lib.List()
	if err != nil {
		return fail(err)
	}
	var shown []library.Entry
	for _, e := range all {
		if *machine == "" || strings.EqualFold(e.Machine, *machine) {
			shown = append(shown, e)
		}
	}
	if *asJSON {
		return printJSON(shown)
	}
	if len(shown) == 0 {
		fmt.Println("No snapshots yet. Save one with `orchard save DIR`, `w` in the TUI, or `orchard remote HOST`.")
		return 0
	}
	tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tMACHINE\tSCANNED\tROOT\tUSED\tFILES\tNOTE")
	now := time.Now()
	for _, e := range shown {
		note := e.Label
		if e.Stats.Partial {
			note = strings.TrimSpace("partial " + note)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s (%s)\t%s\t%s\t%d\t%s\n", e.ID, e.Machine, e.ScannedAt.Local().Format("2006-01-02 15:04"), library.Age(e.ScannedAt, now), e.Root, ui.Bytes(e.Stats.Allocated), e.Stats.Files, note)
	}
	tw.Flush()
	return 0
}

func printJSON(v any) int {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return fail(err)
	}
	return 0
}

func load(lib *library.Library, ref string) (*snapshot.Snapshot, error) {
	e, err := lib.Resolve(ref)
	if err != nil {
		return nil, err
	}
	return library.Load(e)
}

func cmdOpen(ctx context.Context, args []string) int {
	f := newFlags("open")
	apparent := f.Bool("apparent", false, "show logical file lengths")
	pos, ok := parse(f, args)
	if !ok || len(pos) != 1 {
		f.Usage()
		return 2
	}
	lib, err := openLibrary()
	if err != nil {
		return fail(err)
	}
	snap, err := load(lib, pos[0])
	if err != nil {
		return fail(err)
	}
	return runUI(ctx, ui.Config{Apparent: *apparent, Open: snap})
}

func runUI(ctx context.Context, cfg ui.Config) int {
	cfg.Version = version
	if err := ui.Run(ctx, cfg); err != nil {
		return fail(err)
	}
	return 0
}

func cmdLibrary(ctx context.Context, _ []string) int {
	return runUI(ctx, ui.Config{Screen: "library"})
}
func cmdFleet(ctx context.Context, _ []string) int {
	return runUI(ctx, ui.Config{Screen: "machines"})
}

func cmdShow(_ context.Context, args []string) int {
	f := newFlags("show")
	asJSON := f.Bool("json", false, "print the header and top-level entries as JSON")
	apparent := f.Bool("apparent", false, "use logical file lengths")
	pos, ok := parse(f, args)
	if !ok || len(pos) != 1 {
		f.Usage()
		return 2
	}
	lib, err := openLibrary()
	if err != nil {
		return fail(err)
	}
	snap, err := load(lib, pos[0])
	if err != nil {
		return fail(err)
	}
	if *asJSON {
		return printJSON(struct {
			Meta    snapshot.Meta `json:"snapshot"`
			Entries []topEntry    `json:"entries"`
		}{snap.Meta, topEntries(snap.Tree, *apparent)})
	}
	m := snap.Meta
	fmt.Printf("%s · %s (%s) · %s\n", m.ID, m.Machine, m.Hostname, m.Root)
	fmt.Printf("scanned %s by orchard %s on %s/%s via %s\n", m.ScannedAt.Local().Format("2006-01-02 15:04:05 MST"), m.Version, m.OS, m.Arch, m.Source)
	fmt.Printf("%s allocated · %s apparent · %d files · %d dirs · %d unreadable\n", ui.Bytes(m.Stats.Allocated), ui.Bytes(m.Stats.Apparent), m.Stats.Files, m.Stats.Directories, m.Stats.Errors)
	if m.Space != nil {
		fmt.Printf("filesystem: %s free of %s\n", ui.Bytes(m.Space.Free), ui.Bytes(m.Space.Total))
	}
	if m.Label != "" {
		fmt.Println("note:", m.Label)
	}
	fmt.Println()
	tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', tabwriter.AlignRight)
	for i, e := range topEntries(snap.Tree, *apparent) {
		if i == 20 {
			break
		}
		fmt.Fprintf(tw, "%s\t  %s\t\n", ui.Bytes(e.Bytes), e.Name)
	}
	tw.Flush()
	return 0
}

type topEntry struct {
	Name  string `json:"name"`
	Dir   bool   `json:"directory"`
	Bytes uint64 `json:"bytes"`
}

func topEntries(t *scan.Tree, apparent bool) []topEntry {
	var out []topEntry
	for _, e := range t.Snapshot(t.Root, apparent).Entries {
		name := e.Name
		if e.Dir {
			name += "/"
		}
		out = append(out, topEntry{name, e.Dir, e.Size})
	}
	return out
}

func cmdDiff(ctx context.Context, args []string) int {
	f := newFlags("diff")
	text := f.Bool("text", false, "print a text report instead of opening the TUI")
	asJSON := f.Bool("json", false, "print a JSON report")
	top := f.Int("top", 15, "entries per section in text and JSON reports")
	apparent := f.Bool("apparent", false, "compare logical file lengths")
	pos, ok := parse(f, args)
	if !ok || len(pos) != 2 {
		f.Usage()
		return 2
	}
	lib, err := openLibrary()
	if err != nil {
		return fail(err)
	}
	before, err := load(lib, pos[0])
	if err != nil {
		return fail(err)
	}
	after, err := load(lib, pos[1])
	if err != nil {
		return fail(err)
	}
	if !*text && !*asJSON && interactive() {
		return runUI(ctx, ui.Config{Apparent: *apparent, Compare: [2]*snapshot.Snapshot{before, after}})
	}
	root := diff.Compare(before.Tree.Root, after.Tree.Root)
	if *asJSON {
		return printJSON(diffReport(before.Meta, after.Meta, root, *apparent, *top))
	}
	printDiff(os.Stdout, before.Meta, after.Meta, root, *apparent, *top)
	return 0
}

type diffEntry struct {
	Path   string `json:"path"`
	Dir    bool   `json:"directory"`
	Status string `json:"status"`
	Before uint64 `json:"before"`
	After  uint64 `json:"after"`
	Delta  int64  `json:"delta"`
}

func toDiffEntry(n *diff.Node, apparent bool) diffEntry {
	return diffEntry{n.Path(), n.Dir, n.Status.String(), n.Before(apparent), n.After(apparent), n.Delta(apparent)}
}

func diffReport(a, b snapshot.Meta, root *diff.Node, apparent bool, top int) any {
	var children, files []diffEntry
	for i, c := range diff.Sorted(root, apparent, true) {
		if i == top {
			break
		}
		children = append(children, toDiffEntry(c, apparent))
	}
	for _, c := range diff.Top(root, apparent, top) {
		files = append(files, toDiffEntry(c, apparent))
	}
	return struct {
		Before       snapshot.Meta `json:"before"`
		After        snapshot.Meta `json:"after"`
		Total        diffEntry     `json:"total"`
		FilesAdded   uint64        `json:"files_added"`
		FilesRemoved uint64        `json:"files_removed"`
		FilesChanged uint64        `json:"files_changed"`
		TopLevel     []diffEntry   `json:"top_level"`
		Files        []diffEntry   `json:"largest_file_changes"`
	}{a, b, toDiffEntry(root, apparent), root.FilesAdded, root.FilesRemoved, root.FilesChanged, children, files}
}

func signed(d int64) string {
	if d < 0 {
		return "-" + ui.Bytes(uint64(-d))
	}
	return "+" + ui.Bytes(uint64(d))
}

func printDiff(w io.Writer, a, b snapshot.Meta, root *diff.Node, apparent bool, top int) {
	fmt.Fprintf(w, "before  %s · %s · %s · %s\n", a.ID, a.Machine, a.Root, a.ScannedAt.Local().Format("2006-01-02 15:04"))
	fmt.Fprintf(w, "after   %s · %s · %s · %s\n", b.ID, b.Machine, b.Root, b.ScannedAt.Local().Format("2006-01-02 15:04"))
	fmt.Fprintf(w, "\n%s → %s  (%s) · files: %d added · %d removed · %d changed\n", ui.Bytes(root.Before(apparent)), ui.Bytes(root.After(apparent)), signed(root.Delta(apparent)), root.FilesAdded, root.FilesRemoved, root.FilesChanged)
	section := func(title string, nodes []*diff.Node) {
		if len(nodes) == 0 {
			return
		}
		fmt.Fprintf(w, "\n%s\n", title)
		tw := tabwriter.NewWriter(w, 2, 4, 2, ' ', 0)
		for _, n := range nodes {
			name := n.Path()
			if n.Dir {
				name += "/"
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s → %s\t%s\n", signed(n.Delta(apparent)), n.Status, ui.Bytes(n.Before(apparent)), ui.Bytes(n.After(apparent)), name)
		}
		tw.Flush()
	}
	children := diff.Sorted(root, apparent, true)
	section("TOP-LEVEL CHANGES", children[:min(top, len(children))])
	section("LARGEST FILE CHANGES", diff.Top(root, apparent, top))
	if root.Status == diff.Same {
		fmt.Fprintln(w, "\nNo differences.")
	}
}

func cmdDelete(_ context.Context, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: orchard", commands["delete"].usage)
		return 2
	}
	lib, err := openLibrary()
	if err != nil {
		return fail(err)
	}
	code := 0
	for _, ref := range args {
		e, err := lib.Resolve(ref)
		if err == nil {
			err = lib.Delete(e)
		}
		if err != nil {
			code = fail(err)
			continue
		}
		fmt.Printf("deleted %s · %s · %s\n", e.ID, e.Machine, e.Root)
	}
	return code
}

func cmdArchive(_ context.Context, args []string) int {
	usage := func() int {
		fmt.Fprintln(os.Stderr, `Usage:
  orchard archive export [--machine NAME] [--before YYYY-MM-DD] [--remove] FILE [REF...]
  orchard archive import FILE
  orchard archive list FILE

export writes the selected snapshots (all, when nothing selects) to FILE;
--remove then deletes them from the library. import adds an archive's
snapshots back, skipping ones already present.`)
		return 2
	}
	if len(args) == 0 {
		return usage()
	}
	lib, err := openLibrary()
	if err != nil {
		return fail(err)
	}
	switch args[0] {
	case "export":
		f := flag.NewFlagSet("orchard archive export", flag.ContinueOnError)
		machine := f.String("machine", "", "only this machine's snapshots")
		before := f.String("before", "", "only snapshots taken before this date")
		remove := f.Bool("remove", false, "delete the snapshots from the library once archived")
		pos, ok := parse(f, args[1:])
		if !ok || len(pos) < 1 {
			return usage()
		}
		var cutoff time.Time
		if *before != "" {
			if cutoff, err = time.ParseInLocation("2006-01-02", *before, time.Local); err != nil {
				return fail(fmt.Errorf("--before wants YYYY-MM-DD: %w", err))
			}
		}
		var picked []library.Entry
		if len(pos) > 1 {
			for _, ref := range pos[1:] {
				e, err := lib.Resolve(ref)
				if err != nil {
					return fail(err)
				}
				picked = append(picked, e)
			}
		} else if picked, err = lib.List(); err != nil {
			return fail(err)
		}
		var chosen []library.Entry
		for _, e := range picked {
			if *machine != "" && !strings.EqualFold(e.Machine, *machine) || !cutoff.IsZero() && !e.ScannedAt.Before(cutoff) {
				continue
			}
			chosen = append(chosen, e)
		}
		if err := library.Export(pos[0], chosen); err != nil {
			return fail(err)
		}
		fmt.Printf("archived %d snapshots to %s\n", len(chosen), pos[0])
		if *remove {
			for _, e := range chosen {
				if err := lib.Delete(e); err != nil {
					return fail(err)
				}
			}
			fmt.Printf("removed them from the library; restore with: orchard archive import %s\n", pos[0])
		}
		return 0
	case "import":
		if len(args) != 2 {
			return usage()
		}
		res, err := lib.ImportArchive(args[1])
		if err != nil {
			return fail(err)
		}
		for _, m := range res.Added {
			fmt.Printf("imported %s · %s · %s\n", m.ID, m.Machine, m.Root)
		}
		fmt.Printf("%d imported, %d already in the library\n", len(res.Added), len(res.Skipped))
		return 0
	case "list":
		if len(args) != 2 {
			return usage()
		}
		m, err := library.ReadManifest(args[1])
		if err != nil {
			return fail(err)
		}
		tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
		fmt.Fprintf(tw, "archive created %s\n", m.Created.Local().Format("2006-01-02 15:04"))
		fmt.Fprintln(tw, "ID\tMACHINE\tSCANNED\tROOT\tUSED")
		for _, s := range m.Snapshots {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", s.ID, s.Machine, s.ScannedAt.Local().Format("2006-01-02 15:04"), s.Root, ui.Bytes(s.Stats.Allocated))
		}
		tw.Flush()
		return 0
	}
	return usage()
}

type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error { *l = append(*l, v); return nil }

func cmdMachine(_ context.Context, args []string) int {
	usage := func() int {
		fmt.Fprintln(os.Stderr, `Usage:
  orchard machine add NAME [user@]HOST [--port N] [--identity FILE] [--path DIR]...
                      [--option SSH_OPTION]... [--binary REMOTE_PATH] [--sudo] [--notes TEXT]
  orchard machine list [--json]
  orchard machine show NAME
  orchard machine remove NAME

HOST may be an alias from ~/.ssh/config. add replaces a profile with the same name.`)
		return 2
	}
	if len(args) == 0 {
		return usage()
	}
	lib, err := openLibrary()
	if err != nil {
		return fail(err)
	}
	switch args[0] {
	case "add":
		f := flag.NewFlagSet("orchard machine add", flag.ContinueOnError)
		port := f.Int("port", 0, "SSH port")
		identity := f.String("identity", "", "SSH identity file")
		binary := f.String("binary", "", "path to orchard on the remote machine")
		sudo := f.Bool("sudo", false, "scan with sudo -n (needs passwordless sudo for orchard)")
		notes := f.String("notes", "", "free-form notes")
		var paths, options listFlag
		f.Var(&paths, "path", "directory to scan by default (repeatable; the first is the default)")
		f.Var(&options, "option", "extra ssh -o option (repeatable)")
		pos, ok := parse(f, args[1:])
		if !ok || len(pos) != 2 {
			return usage()
		}
		m := library.Machine{Name: pos[0], Host: pos[1], Port: *port, Identity: *identity, Binary: *binary, Sudo: *sudo, Notes: *notes, Paths: paths, Options: options}
		if err := lib.SaveMachine(m); err != nil {
			return fail(err)
		}
		fmt.Printf("saved machine %s (%s). Scan it with: orchard remote %s\n", m.Name, m.Host, m.Name)
		return 0
	case "list":
		f := flag.NewFlagSet("orchard machine list", flag.ContinueOnError)
		asJSON := f.Bool("json", false, "print JSON")
		if _, ok := parse(f, args[1:]); !ok {
			return 2
		}
		ms, err := lib.Machines()
		if err != nil {
			return fail(err)
		}
		if *asJSON {
			return printJSON(ms)
		}
		all, _ := lib.List()
		tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "NAME\tHOST\tDEFAULT PATH\tSNAPSHOTS\tLAST SCAN")
		local := snapshot.Hostname()
		fmt.Fprintf(tw, "%s\t(this machine)\t\t%s\n", local, lastScan(all, local))
		for _, m := range ms {
			host := m.Host
			if m.Port != 0 {
				host += fmt.Sprintf(":%d", m.Port)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", m.Name, host, m.DefaultPath(), lastScan(all, m.Name))
		}
		tw.Flush()
		return 0
	case "show":
		if len(args) != 2 {
			return usage()
		}
		m, err := lib.Machine(args[1])
		if err != nil {
			return fail(err)
		}
		return printJSON(m)
	case "remove", "rm":
		if len(args) != 2 {
			return usage()
		}
		if err := lib.RemoveMachine(args[1]); err != nil {
			return fail(err)
		}
		fmt.Printf("removed machine %s; its snapshots stay in the library\n", args[1])
		return 0
	}
	return usage()
}

func lastScan(all []library.Entry, machine string) string {
	count := 0
	var last library.Entry
	for _, e := range all {
		if strings.EqualFold(e.Machine, machine) {
			if count == 0 {
				last = e
			}
			count++
		}
	}
	if count == 0 {
		return "0\tnever"
	}
	return fmt.Sprintf("%d\t%s · %s · %s", count, library.Age(last.ScannedAt, time.Now()), last.Root, ui.Bytes(last.Stats.Allocated))
}

// targetFor turns a profile name or an ssh destination into a target and
// the machine name its snapshots are filed under.
func targetFor(lib *library.Library, name string) (remote.Target, library.Machine) {
	m, err := lib.Machine(name)
	if err != nil {
		// Not a saved profile: treat it as an ssh destination and file its
		// snapshots under the host part.
		host := name
		if _, h, ok := strings.Cut(name, "@"); ok {
			host = h
		}
		m = library.Machine{Name: library.SafeName(host), Host: name}
	}
	return remote.Target{Host: m.Host, Port: m.Port, Identity: m.Identity, Options: m.Options, Binary: m.Binary, Sudo: m.Sudo}, m
}

func cmdRemote(ctx context.Context, args []string) int {
	f := newFlags("remote")
	noOpen := f.Bool("no-open", false, "save the snapshot without opening it")
	upload := f.String("upload", "", "local orchard build to install remotely (for another OS or architecture)")
	label := f.String("label", "", "a note to keep with the snapshot")
	pos, ok := parse(f, args)
	if !ok || len(pos) < 1 || len(pos) > 2 {
		f.Usage()
		return 2
	}
	lib, err := openLibrary()
	if err != nil {
		return fail(err)
	}
	target, m := targetFor(lib, pos[0])
	target.Upload = *upload
	path := m.DefaultPath()
	if len(pos) == 2 {
		path = pos[1]
	}
	tty := term.IsTerminal(int(os.Stderr.Fd()))
	target.Stage = func(s string) {
		if tty {
			fmt.Fprintf(os.Stderr, "\r\033[K%s…", s)
		} else {
			fmt.Fprintln(os.Stderr, s)
		}
	}
	snap, err := target.Scan(ctx, m.Name, path, func(p remote.Progress) {
		if tty {
			fmt.Fprintf(os.Stderr, "\r\033[Kscanning %s:%s · %d files · %d dirs · %s", m.Name, path, p.Files, p.Directories, ui.Bytes(p.Bytes))
		}
	})
	if tty {
		fmt.Fprint(os.Stderr, "\r\033[K")
	}
	if err != nil {
		return fail(err)
	}
	snap.Meta.Label = *label
	e, err := lib.Save(snap.Meta, snap.Tree)
	if err != nil {
		return fail(err)
	}
	fmt.Printf("saved snapshot %s · %s (%s) · %s · %s · %d files\n", e.ID, e.Machine, e.Hostname, e.Root, ui.Bytes(e.Stats.Allocated), e.Stats.Files)
	if *noOpen || !interactive() {
		return 0
	}
	return runUI(ctx, ui.Config{Open: snap})
}

func cmdQuery(_ context.Context, args []string) int {
	f := newFlags("query")
	all := f.Bool("all", false, "search every snapshot, not just the newest per machine and root")
	asJSON := f.Bool("json", false, "print JSON")
	limit := f.Int("limit", 50, "most results to list (0 for all)")
	group := f.String("group", "", "sum matches by machine or ext instead of listing them")
	apparent := f.Bool("apparent", false, "use logical file lengths")
	pos, ok := parse(f, args)
	if !ok || len(pos) == 0 {
		f.Usage()
		fmt.Fprintln(os.Stderr, "\nExamples:\n  orchard query 'ext:iso size>1G'\n  orchard query --group machine 'path:/var/log type:file'\n  orchard query 'type:dir node_modules' --all\n\nTerms: name re: path: ext: type:file|dir machine: size>1G modified<2024-01-01 age>90d; -term negates.")
		return 2
	}
	q, err := query.Parse(strings.Join(pos, " "))
	if err != nil {
		return fail(err)
	}
	q.Apparent = *apparent
	lib, err := openLibrary()
	if err != nil {
		return fail(err)
	}
	entries, err := lib.List()
	if err != nil {
		return fail(err)
	}
	if !*all {
		entries = library.Latest(entries)
	}
	var scope []library.Entry
	var trees []*scan.Tree
	for _, e := range entries {
		if !q.WantsMachine(e.Machine) {
			continue
		}
		snap, err := library.Load(e)
		if err != nil {
			fmt.Fprintf(os.Stderr, "orchard: skipping %s: %v\n", e.ID, err)
			continue
		}
		scope = append(scope, e)
		trees = append(trees, snap.Tree)
	}
	if len(trees) == 0 {
		return fail(errors.New("no snapshots in scope; save some first"))
	}
	keep := *limit
	if *group != "" {
		keep = 0
	}
	res := q.Run(trees, keep, time.Now())
	if *group != "" {
		return printGroups(res, scope, *group, *asJSON, !strings.Contains(q.Source, "type:f") && !strings.Contains(q.Source, "ext:"))
	}
	type hit struct {
		Machine  string    `json:"machine"`
		Snapshot string    `json:"snapshot"`
		Root     string    `json:"root"`
		Path     string    `json:"path"`
		Dir      bool      `json:"directory"`
		Bytes    uint64    `json:"bytes"`
		Modified time.Time `json:"modified"`
	}
	hits := make([]hit, 0, len(res.Hits))
	for _, h := range res.Hits {
		e := scope[h.Snapshot]
		hits = append(hits, hit{e.Machine, e.ID, e.Root, h.Path, h.Node.Dir, h.Size, time.Unix(h.Node.Modified, 0)})
	}
	if *asJSON {
		return printJSON(struct {
			Query     string `json:"query"`
			Snapshots int    `json:"snapshots_searched"`
			Matches   int    `json:"matches"`
			Hits      []hit  `json:"hits"`
		}{q.Source, len(trees), res.Matches, hits})
	}
	tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "MACHINE\tSIZE\tMODIFIED\tPATH")
	for _, h := range hits {
		name := filepath.Join(h.Root, h.Path)
		if h.Dir {
			name += "/"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", h.Machine, ui.Bytes(h.Bytes), h.Modified.Local().Format("2006-01-02"), name)
	}
	tw.Flush()
	fmt.Printf("\n%d matches in %d snapshots", res.Matches, len(trees))
	if res.Matches > len(hits) {
		fmt.Printf(" · showing the largest %d (--limit)", len(hits))
	}
	fmt.Println()
	return 0
}

func printGroups(res query.Result, scope []library.Entry, by string, asJSON, nested bool) int {
	type group struct {
		Key   string `json:"key"`
		Count int    `json:"count"`
		Bytes uint64 `json:"bytes"`
	}
	groups := map[string]*group{}
	for _, h := range res.Hits {
		var key string
		switch by {
		case "machine":
			key = scope[h.Snapshot].Machine
		case "ext":
			key = strings.ToLower(filepath.Ext(h.Node.Name))
			if h.Node.Dir {
				key = "(directory)"
			} else if key == "" {
				key = "(none)"
			}
		default:
			return fail(fmt.Errorf("--group wants machine or ext, not %q", by))
		}
		g := groups[key]
		if g == nil {
			g = &group{Key: key}
			groups[key] = g
		}
		g.Count++
		g.Bytes += h.Size
	}
	out := make([]group, 0, len(groups))
	for _, g := range groups {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Bytes > out[j].Bytes })
	if asJSON {
		return printJSON(out)
	}
	tw := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "%s\tMATCHES\tBYTES\n", strings.ToUpper(by))
	for _, g := range out {
		fmt.Fprintf(tw, "%s\t%d\t%s\n", g.Key, g.Count, ui.Bytes(g.Bytes))
	}
	tw.Flush()
	if nested {
		fmt.Println("\nSums count nested matches more than once; add type:file for a true total.")
	}
	return 0
}
