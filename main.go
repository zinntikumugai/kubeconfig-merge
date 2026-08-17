// Command kubeconfig-merge merges selected contexts from several kubeconfig
// files into a single ./config, renaming clusters, users and contexts along the
// way. It operates on the current working directory.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

type options struct {
	dryRun   bool
	flatten  bool
	noBackup bool
	verbose  bool
}

const usageHeader = `kubeconfig-merge merges selected contexts from *.kconfig.yaml files
into ./config, following the rules in ./kconfig.yaml.

Usage:
  kubeconfig-merge [flags]

Flags:
`

// cli parses args and runs the tool. It returns the process exit code.
func cli(args []string, workDir string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("kubeconfig-merge", flag.ContinueOnError)
	// Print the usage ourselves so that help goes to stdout and errors to
	// stderr, instead of everything landing on stderr twice.
	fs.SetOutput(io.Discard)
	usage := func(w io.Writer) {
		fmt.Fprint(w, usageHeader)
		fs.SetOutput(w)
		fs.PrintDefaults()
		fs.SetOutput(io.Discard)
	}
	fs.Usage = func() {}

	var (
		opts        options
		showVersion bool
	)
	fs.BoolVar(&opts.dryRun, "dry-run", false, "validate and show the merge result without touching any file")
	fs.BoolVar(&opts.flatten, "flatten", false, "embed certificate/key files as data (overrides options.flatten)")
	fs.BoolVar(&opts.noBackup, "no-backup", false, "do not back up an existing ./config before replacing it")
	fs.BoolVar(&opts.verbose, "verbose", false, "log what is being loaded, resolved and written")
	fs.BoolVar(&showVersion, "version", false, "print the version and exit")

	switch err := fs.Parse(args); {
	case err == flag.ErrHelp:
		usage(stdout)
		return 0
	case err != nil:
		fmt.Fprintf(stderr, "kubeconfig-merge: %v\n", err)
		usage(stderr)
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "kubeconfig-merge: unexpected argument %q\n", fs.Arg(0))
		usage(stderr)
		return 2
	}

	if showVersion {
		fmt.Fprintf(stdout, "kubeconfig-merge %s (%s %s/%s)\n", version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
		return 0
	}

	if err := run(workDir, opts, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "kubeconfig-merge: %v\n", err)
		return 1
	}
	return 0
}

func main() {
	workDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "kubeconfig-merge: %v\n", err)
		os.Exit(1)
	}
	os.Exit(cli(os.Args[1:], workDir, os.Stdout, os.Stderr))
}
