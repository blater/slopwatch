// slopcache performs explicit developer maintenance of an analysis cache.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"github.com/blater/slopwatch/internal/analysiscache"
)

func run(ctx context.Context, args []string, out, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "compact" {
		fmt.Fprintln(stderr, "usage: slopcache compact --root <cache-directory>")
		return 2
	}
	flags := flag.NewFlagSet("compact", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "explicit existing analysis cache directory")
	if err := flags.Parse(args[1:]); err != nil {
		return 2
	}
	if strings.TrimSpace(*root) == "" || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "compact requires an explicit --root and no positional arguments")
		return 2
	}
	info, err := os.Stat(*root)
	if err != nil || !info.IsDir() {
		fmt.Fprintln(stderr, "cache root must be an existing directory")
		return 2
	}
	for _, name := range []string{"artifacts", "sources", "units", "workspaces"} {
		info, err := os.Lstat(filepath.Join(*root, name))
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			fmt.Fprintf(stderr, "cache root requires existing nonsymlink %s directory\n", name)
			return 2
		}
	}
	store, err := analysiscache.NewStore(*root)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	stats, err := store.CompactArtifacts(ctx)
	fmt.Fprintf(out, "artifacts=%d compacted=%d skipped=%d errors=%d bytes_before=%d bytes_after=%d\n", stats.Examined, stats.Compacted, stats.Skipped, stats.Errors, stats.BeforeBytes, stats.AfterBytes)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}
