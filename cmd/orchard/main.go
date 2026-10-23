package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ob1rao/orchard/internal/remote"
	"github.com/ob1rao/orchard/internal/scan"
	"github.com/ob1rao/orchard/internal/snapshot"
	"github.com/ob1rao/orchard/internal/ui"
	"github.com/ob1rao/orchard/internal/volumes"
)

var version = "dev"

func main() { os.Exit(run()) }
func run() int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if len(os.Args) > 1 {
		if cmd, ok := commands[os.Args[1]]; ok {
			return cmd.run(ctx, os.Args[2:])
		}
	}
	flags := flag.NewFlagSet("orchard", flag.ContinueOnError)
	apparent := flags.Bool("apparent", false, "show logical file lengths instead of allocated blocks")
	unmounted := flags.Bool("unmounted", false, "include unmounted volumes in the disk picker")
	headless := flags.Bool("scan", false, "scan a path and print a JSON summary without a TUI")
	save := flags.Bool("save", false, "with --scan: also save the scan to the snapshot library")
	label := flags.String("label", "", "with --save: a note to keep with the snapshot")
	emit := flags.Bool("emit-snapshot", false, "with --scan: write a binary snapshot to stdout instead of JSON (used by remote scans)")
	progress := flags.Bool("progress", false, "with --scan: report progress on stderr")
	protocol := flags.Bool("remote-protocol", false, "print the remote scan protocol this build speaks")
	workers := flags.Int("workers", 0, "directory workers (1-64; 0 tunes the count to the disk)")
	ver := flags.Bool("version", false, "print version")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: orchard [options] [directory]\n       orchard <command> [arguments]\n\nLaunch without a directory to select a mounted disk.\n\nOptions:")
		flags.PrintDefaults()
		fmt.Fprintln(flags.Output(), "\nCommands:")
		printCommands(flags.Output())
	}
	if err := flags.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if *ver {
		fmt.Println("orchard " + version)
		return 0
	}
	if *protocol {
		fmt.Println(remote.Banner)
		return 0
	}
	if flags.NArg() > 1 || *workers < 0 || *workers > 64 {
		fmt.Fprintln(os.Stderr, "orchard: supply at most one path and 0-64 workers")
		return 2
	}
	path := flags.Arg(0)
	opt := scan.Options{Workers: *workers}
	if *headless {
		if path == "" {
			fmt.Fprintln(os.Stderr, "orchard: --scan requires a directory")
			return 2
		}
		return headlessScan(ctx, path, opt, *apparent, *emit, *progress, *save, *label)
	}
	if err := ui.Run(ctx, ui.Config{Path: path, Apparent: *apparent, Unmounted: *unmounted, Scan: opt, Version: version}); err != nil {
		fmt.Fprintln(os.Stderr, "orchard:", err)
		return 1
	}
	return 0
}

// scanToDone runs a scan to completion, reporting progress while it runs.
func scanToDone(ctx context.Context, path string, opt scan.Options, report func(scan.Stats)) (*scan.Tree, time.Time, error) {
	started := time.Now()
	t, done, err := scan.Start(ctx, path, opt)
	if err != nil {
		return nil, started, err
	}
	if report == nil {
		<-done
		return t, started, nil
	}
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-done:
			report(t.Stats())
			return t, started, nil
		case <-tick.C:
			report(t.Stats())
		}
	}
}

// describe builds a snapshot header for a local scan, including the
// filesystem's capacity so an offline view can still show free space.
func describe(t *scan.Tree, started time.Time, label string) snapshot.Meta {
	meta := snapshot.Describe(t, started, version)
	meta.Label = label
	if space, err := volumes.ReadSpace(t.Root.Path()); err == nil {
		meta.Space = &space
	}
	return meta
}

func headlessScan(ctx context.Context, path string, opt scan.Options, apparent, emit, progress, save bool, label string) int {
	var report func(scan.Stats)
	if progress {
		enc := json.NewEncoder(os.Stderr)
		report = func(s scan.Stats) {
			fmt.Fprint(os.Stderr, remote.ProgressPrefix)
			_ = enc.Encode(remote.Progress{Files: s.Files, Directories: s.Directories, Bytes: s.Allocated, Errors: s.Errors})
		}
	}
	t, started, err := scanToDone(ctx, path, opt, report)
	if err != nil {
		fmt.Fprintln(os.Stderr, "orchard:", err)
		return 1
	}
	if emit {
		// Unreadable entries are normal on a whole-disk scan; they travel in
		// the snapshot's stats rather than failing the transfer.
		if err := snapshot.Write(os.Stdout, describe(t, started, label), t); err != nil {
			fmt.Fprintln(os.Stderr, "orchard:", err)
			return 1
		}
		return 0
	}
	if save {
		if code := saveTree(t, started, label, os.Stderr); code != 0 {
			return code
		}
	}
	return printSummary(os.Stdout, t, apparent)
}

func printSummary(w io.Writer, t *scan.Tree, apparent bool) int {
	v := t.Snapshot(t.Root, apparent)
	type entry struct {
		Name      string  `json:"name"`
		Directory bool    `json:"directory"`
		Bytes     uint64  `json:"bytes"`
		Modified  string  `json:"modified"`
		Created   *string `json:"created"`
	}
	report := struct {
		Path     string     `json:"path"`
		Metric   string     `json:"metric"`
		Bytes    uint64     `json:"bytes"`
		Stats    scan.Stats `json:"stats"`
		Children []entry    `json:"children"`
	}{Path: t.Root.Path(), Metric: "allocated", Bytes: v.Size, Stats: v.Stats, Children: make([]entry, 0, len(v.Entries))}
	if apparent {
		report.Metric = "apparent"
	}
	for _, e := range v.Entries {
		var created *string
		if e.Node.HasCreated {
			value := time.Unix(e.Node.Created, 0).UTC().Format(time.RFC3339)
			created = &value
		}
		report.Children = append(report.Children, entry{Name: e.Name, Directory: e.Dir, Bytes: e.Size, Modified: time.Unix(e.Node.Modified, 0).UTC().Format(time.RFC3339), Created: created})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if v.Stats.Cancelled {
		return 130
	}
	if v.Stats.Errors > 0 {
		return 1
	}
	return 0
}
