package main

import (
	"bytes"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the want-config.yaml golden files")

const scenarioRoot = "testdata/scenarios"

// TestScenarios runs the tool over the real input files under
// testdata/scenarios. Directories named ok-* must merge cleanly and produce
// exactly want-config.yaml; ng-* must fail with want-error.txt and leave the
// existing config alone.
func TestScenarios(t *testing.T) {
	entries, err := os.ReadDir(scenarioRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatalf("no scenarios in %s", scenarioRoot)
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			switch {
			case strings.HasPrefix(name, "ok-"):
				runOKScenario(t, name)
			case strings.HasPrefix(name, "ng-"):
				runNGScenario(t, name)
			default:
				t.Fatalf("scenario %q must be named ok-* or ng-*", name)
			}
		})
	}
}

func runOKScenario(t *testing.T, name string) {
	t.Helper()
	src := filepath.Join(scenarioRoot, name)
	work := copyScenario(t, src)
	previous, hadConfig := readFile(t, filepath.Join(work, "config"))

	var stdout, stderr bytes.Buffer
	if err := run(work, options{}, &stdout, &stderr); err != nil {
		t.Fatalf("run: %v", err)
	}

	got, ok := readFile(t, filepath.Join(work, "config"))
	if !ok {
		t.Fatal("no config was written")
	}
	goldenPath := filepath.Join(src, "want-config.yaml")
	if *update {
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, ok := readFile(t, goldenPath)
	if !ok {
		t.Fatalf("missing golden %s (re-run with -update)", goldenPath)
	}
	if got != want {
		t.Errorf("config does not match %s\n--- got ---\n%s\n--- want ---\n%s", goldenPath, got, want)
	}

	fi, err := os.Stat(filepath.Join(work, "config"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("config mode = %o, want 600", perm)
	}

	// A pre-existing config must have been backed up verbatim.
	backups := listDir(t, filepath.Join(work, "backup"))
	if hadConfig {
		if len(backups) != 1 {
			t.Fatalf("got %d backups, want 1: %v", len(backups), backups)
		}
		if c, _ := readFile(t, filepath.Join(work, "backup", backups[0])); c != previous {
			t.Errorf("backup content = %q, want the previous config", c)
		}
	} else if len(backups) != 0 {
		t.Errorf("backups created for a fresh directory: %v", backups)
	}

	// The summary lists every produced context.
	for _, line := range []string{"SOURCE", "OUTPUT CONTEXT"} {
		if !strings.Contains(stdout.String(), line) {
			t.Errorf("summary is missing %q:\n%s", line, stdout.String())
		}
	}
}

func runNGScenario(t *testing.T, name string) {
	t.Helper()
	src := filepath.Join(scenarioRoot, name)
	work := copyScenario(t, src)
	before := snapshot(t, work)

	var stdout, stderr bytes.Buffer
	err := run(work, options{}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run succeeded, want an error")
	}
	wantErr, ok := readFile(t, filepath.Join(src, "want-error.txt"))
	if !ok {
		t.Fatalf("missing %s/want-error.txt", src)
	}
	if want := strings.TrimSpace(wantErr); !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q\nwant it to contain %q", err, want)
	}

	// A failed merge must not have touched anything on disk.
	if diff := diffSnapshot(before, snapshot(t, work)); diff != "" {
		t.Errorf("files changed despite the error: %s", diff)
	}
}

// TestScenariosDryRun replays every scenario with --dry-run: the outcome must
// be the same, and no file may change.
func TestScenariosDryRun(t *testing.T) {
	entries, err := os.ReadDir(scenarioRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			work := copyScenario(t, filepath.Join(scenarioRoot, name))
			before := snapshot(t, work)

			var stdout, stderr bytes.Buffer
			err := run(work, options{dryRun: true}, &stdout, &stderr)
			if strings.HasPrefix(name, "ok-") && err != nil {
				t.Fatalf("dry-run: %v", err)
			}
			if strings.HasPrefix(name, "ng-") && err == nil {
				t.Fatal("dry-run succeeded, want an error")
			}
			if diff := diffSnapshot(before, snapshot(t, work)); diff != "" {
				t.Errorf("dry-run modified the working directory: %s", diff)
			}
		})
	}
}

// copyScenario copies a scenario into a scratch directory, leaving the
// want-* expectation files behind.
func copyScenario(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if strings.HasPrefix(d.Name(), "want-") {
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func diffSnapshot(before, after map[string]string) string {
	var diffs []string
	for name, content := range after {
		switch old, ok := before[name]; {
		case !ok:
			diffs = append(diffs, "created "+name)
		case old != content:
			diffs = append(diffs, "modified "+name)
		}
	}
	for name := range before {
		if _, ok := after[name]; !ok {
			diffs = append(diffs, "removed "+name)
		}
	}
	return strings.Join(diffs, ", ")
}

func readFile(t *testing.T, path string) (string, bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data), true
}

func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}
