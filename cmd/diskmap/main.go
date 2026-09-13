package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ob1rao/diskmap/internal/scan"
	"github.com/ob1rao/diskmap/internal/ui"
)

var version = "dev"

func main() { os.Exit(run()) }
func run() int {
	flags := flag.NewFlagSet("diskmap", flag.ContinueOnError)
	apparent := flags.Bool("apparent", false, "show logical file lengths instead of allocated blocks")
	headless := flags.Bool("scan", false, "scan a path and print a JSON summary without a TUI")
	workers := flags.Int("workers", scan.DefaultWorkers(), "directory workers (1-64)")
	ver := flags.Bool("version", false, "print version")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: diskmap [options] [directory]\n\nLaunch without a directory to select a mounted disk.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if *ver {
		fmt.Println("diskmap " + version)
		return 0
	}
	if flags.NArg() > 1 || *workers < 1 || *workers > 64 {
		fmt.Fprintln(os.Stderr, "diskmap: supply at most one path and 1-64 workers")
		return 2
	}
	path := flags.Arg(0)
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	opt := scan.Options{Workers: *workers}
	if *headless {
		if path == "" {
			fmt.Fprintln(os.Stderr, "diskmap: --scan requires a directory")
			return 2
		}
		t, done, err := scan.Start(ctx, path, opt)
		if err != nil {
			fmt.Fprintln(os.Stderr, "diskmap:", err)
			return 1
		}
		<-done
		v := t.Snapshot(t.Root, *apparent)
		type entry struct {
			Name      string `json:"name"`
			Directory bool   `json:"directory"`
			Bytes     uint64 `json:"bytes"`
		}
		report := struct {
			Path     string     `json:"path"`
			Metric   string     `json:"metric"`
			Bytes    uint64     `json:"bytes"`
			Stats    scan.Stats `json:"stats"`
			Children []entry    `json:"children"`
		}{Path: t.Root.Path(), Metric: "allocated", Bytes: v.Size, Stats: v.Stats, Children: make([]entry, 0, len(v.Entries))}
		if *apparent {
			report.Metric = "apparent"
		}
		for _, e := range v.Entries {
			report.Children = append(report.Children, entry{e.Name, e.Dir, e.Size})
		}
		enc := json.NewEncoder(os.Stdout)
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
	if err := ui.Run(ctx, path, *apparent, opt); err != nil {
		fmt.Fprintln(os.Stderr, "diskmap:", err)
		return 1
	}
	return 0
}
