package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCLIVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := cli([]string{"--version"}, t.TempDir(), &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	got := stdout.String()
	for _, want := range []string{"kubeconfig-merge", version, runtime.Version(), runtime.GOOS + "/" + runtime.GOARCH} {
		if !strings.Contains(got, want) {
			t.Errorf("version output %q does not contain %q", got, want)
		}
	}
}

func TestCLIHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := cli([]string{"--help"}, t.TempDir(), &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	got := stdout.String()
	for _, want := range []string{"-dry-run", "-flatten", "-no-backup", "-verbose", "-version"} {
		if !strings.Contains(got, want) {
			t.Errorf("help output does not document %q:\n%s", want, got)
		}
	}
	// Asking for help is not an error, and the usage must not be printed twice.
	if stderr.Len() != 0 {
		t.Errorf("--help wrote to stderr: %q", stderr.String())
	}
}

func TestCLIUnknownFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := cli([]string{"--nope"}, t.TempDir(), &stdout, &stderr); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("unknown flag wrote to stdout: %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "nope") {
		t.Errorf("stderr does not mention the bad flag: %q", stderr.String())
	}
}

// A profile nobody references is not an error, but --verbose must say so: it
// is how a typo in kconfig.yaml shows up.
func TestRunReportsUnusedProfiles(t *testing.T) {
	dir := t.TempDir()
	source, err := os.ReadFile(filepath.Join("testdata", "scenarios", "ok-basic", "merino.kconfig.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "merino.kconfig.yaml"), source, 0o600); err != nil {
		t.Fatal(err)
	}
	kconfig := `
version: 1
sources:
  merino:
    contexts:
      - source: default
        profile: merino-prod
profiles:
  merino-prod:
    cluster: {name: cluster-merino}
    user: {name: cluster-merino-admin}
    context: {name: cluster-merino-admin}
  forgotten-profile:
    cluster: {name: cluster-old}
    user: {name: cluster-old-admin}
    context: {name: cluster-old-admin}
`
	if err := os.WriteFile(filepath.Join(dir, "kconfig.yaml"), []byte(kconfig), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if err := run(dir, options{dryRun: true, verbose: true}, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(stderr.String(), "forgotten-profile") {
		t.Errorf("--verbose does not mention the unused profile:\n%s", stderr.String())
	}
	if strings.Contains(stderr.String(), "merino-prod\"") {
		t.Errorf("the used profile was reported as unused:\n%s", stderr.String())
	}
}

func TestCLIUnexpectedArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := cli([]string{"extra"}, t.TempDir(), &stdout, &stderr); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "extra") {
		t.Errorf("stderr does not mention the unexpected argument: %q", stderr.String())
	}
}
