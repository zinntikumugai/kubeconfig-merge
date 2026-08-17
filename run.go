package main

import (
	"fmt"
	"io"
	"log/slog"
	"maps"
	"path/filepath"
	"slices"
	"text/tabwriter"
	"time"

	"k8s.io/client-go/tools/clientcmd"

	"github.com/zinntikumugai/kubeconfig-merge/internal/atomicfile"
	"github.com/zinntikumugai/kubeconfig-merge/internal/config"
	"github.com/zinntikumugai/kubeconfig-merge/internal/merge"
)

// run performs the whole merge for workDir: read kconfig.yaml, build the merged
// kubeconfig, report it and, unless this is a dry run, back up and replace
// ./config. Every check happens before anything is written, so a failure leaves
// the directory exactly as it was.
func run(workDir string, opts options, stdout, stderr io.Writer) error {
	log := slog.New(slog.DiscardHandler)
	if opts.verbose {
		log = slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}

	cfg, cfgPath, err := config.Load(workDir)
	if err != nil {
		return err
	}
	log.Info("loading config", "file", cfgPath)
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(cfgPath), err)
	}

	res, err := merge.Build(workDir, cfg, opts.flatten || cfg.Options.Flatten, log)
	if err != nil {
		return err
	}
	// A profile nobody referenced is usually a typo in kconfig.yaml, but it is
	// harmless, so only --verbose mentions it.
	for _, name := range unusedProfiles(cfg, res) {
		log.Info("unused profile", "profile", name)
	}
	printSummary(stdout, res)

	configPath := filepath.Join(workDir, "config")
	if opts.dryRun {
		fmt.Fprintf(stdout, "dry-run: %s was not modified\n", configPath)
		return nil
	}

	data, err := clientcmd.Write(*res.Config)
	if err != nil {
		return fmt.Errorf("serializing the merged kubeconfig: %w", err)
	}

	if !opts.noBackup {
		backup, err := atomicfile.Backup(configPath, filepath.Join(workDir, "backup"), time.Now())
		if err != nil {
			return err
		}
		if backup != "" {
			log.Info("backed up the previous config", "file", backup)
			fmt.Fprintf(stdout, "backed up the previous config to %s\n", backup)
		}
	}
	if err := atomicfile.Write(configPath, data); err != nil {
		return err
	}
	log.Info("wrote config", "file", configPath, "contexts", len(res.Entries))
	fmt.Fprintf(stdout, "wrote %s (%d contexts)\n", configPath, len(res.Entries))
	return nil
}

// unusedProfiles returns, sorted, the profiles that no selected context used.
func unusedProfiles(cfg *config.Config, res *merge.Result) []string {
	used := make(map[string]bool, len(res.Entries))
	for _, e := range res.Entries {
		used[e.ProfileName] = true
	}
	var unused []string
	for _, name := range slices.Sorted(maps.Keys(cfg.Profiles)) {
		if !used[name] {
			unused = append(unused, name)
		}
	}
	return unused
}

// printSummary prints one line per merged context, in merge order.
func printSummary(w io.Writer, res *merge.Result) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SOURCE\tSOURCE CONTEXT\tOUTPUT CONTEXT\tCLUSTER\tSERVER")
	for _, e := range res.Entries {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", e.SourceID, e.SourceContext, e.ContextName, e.ClusterName, e.Server)
	}
	tw.Flush()
	fmt.Fprintln(w)
}
