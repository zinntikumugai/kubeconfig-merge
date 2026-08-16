package atomicfile

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

var testTime = time.Date(2026, 8, 17, 1, 31, 0, 0, time.UTC)

func TestWriteCreatesFile0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")

	if err := Write(path, []byte("hello")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("content = %q, want %q", got, "hello")
	}
	assertMode0600(t, path)
	assertNoLeftovers(t, dir, "config")
}

func TestWriteReplacesExistingFileAndForces0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Write(path, []byte("new")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "new" {
		t.Errorf("content = %q, want %q", got, "new")
	}
	// A kubeconfig may hold private keys: never inherit a loose mode.
	assertMode0600(t, path)
	assertNoLeftovers(t, dir, "config")
}

func TestBackupNoExistingFile(t *testing.T) {
	dir := t.TempDir()
	backupDir := filepath.Join(dir, "backup")

	got, err := Backup(filepath.Join(dir, "config"), backupDir, testTime)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if got != "" {
		t.Errorf("Backup path = %q, want empty", got)
	}
	if _, err := os.Stat(backupDir); !os.IsNotExist(err) {
		t.Errorf("backup dir was created for a non-existent source file")
	}
}

func TestBackupCopiesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	backupDir := filepath.Join(dir, "backup")
	if err := os.WriteFile(path, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := Backup(path, backupDir, testTime)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}

	want := filepath.Join(backupDir, "config.20260817-013100")
	if got != want {
		t.Fatalf("Backup path = %q, want %q", got, want)
	}
	content, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(content) != "secret" {
		t.Errorf("backup content = %q, want %q", content, "secret")
	}
	assertMode0600(t, got)
	// The original must survive untouched.
	if _, err := os.Stat(path); err != nil {
		t.Errorf("original file gone after backup: %v", err)
	}
}

func TestBackupSameSecondDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	backupDir := filepath.Join(dir, "backup")

	if err := os.WriteFile(path, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := Backup(path, backupDir, testTime)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}

	if err := os.WriteFile(path, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := Backup(path, backupDir, testTime)
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}

	if first == second {
		t.Fatalf("second backup reused the same name %q", second)
	}
	if want := filepath.Join(backupDir, "config.20260817-013100-1"); second != want {
		t.Errorf("second backup = %q, want %q", second, want)
	}
	if c, _ := os.ReadFile(first); string(c) != "first" {
		t.Errorf("first backup was clobbered: %q", c)
	}
	if c, _ := os.ReadFile(second); string(c) != "second" {
		t.Errorf("second backup content = %q, want %q", c, "second")
	}
}

func assertMode0600(t *testing.T, path string) {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat %s: %v", path, err)
	}
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Errorf("%s mode = %o, want 600", path, got)
	}
}

// assertNoLeftovers checks that no temporary file survived in dir.
func assertNoLeftovers(t *testing.T, dir string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	keep := map[string]bool{}
	for _, w := range want {
		keep[w] = true
	}
	for _, e := range entries {
		if !keep[e.Name()] {
			t.Errorf("unexpected leftover file %q in %s", e.Name(), dir)
		}
	}
}
