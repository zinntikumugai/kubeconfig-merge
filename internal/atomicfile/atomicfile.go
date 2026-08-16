// Package atomicfile writes kubeconfig files without ever leaving a partially
// written file behind, and keeps timestamped copies of what it replaces.
//
// Everything it creates is mode 0600: kubeconfig files may embed client keys
// and tokens.
package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// FileMode is the permission used for every file this package creates.
const FileMode os.FileMode = 0o600

// backupStamp is the timestamp layout used for backup file names.
const backupStamp = "20060102-150405"

// Write atomically replaces path with data: the content is written to a
// temporary file in the same directory, flushed to disk, and then renamed over
// path. If anything fails, path is left untouched.
func Write(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temporary file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() {
		tmp.Close()
		os.Remove(tmpName) // no-op once the rename succeeded
	}()

	// CreateTemp already uses 0600, but the umask does not apply to a chmod,
	// so be explicit rather than rely on it.
	if err := tmp.Chmod(FileMode); err != nil {
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("writing %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("syncing %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replacing %s: %w", path, err)
	}
	return nil
}

// Backup copies path into backupDir as "<name>.<YYYYMMDD-HHMMSS>" and returns
// the backup path. It returns an empty path when path does not exist. Existing
// backups are never overwritten: a "-N" suffix is appended on collision.
func Backup(path, backupDir string, now time.Time) (string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}

	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return "", fmt.Errorf("creating %s: %w", backupDir, err)
	}

	base := filepath.Join(backupDir, filepath.Base(path)+"."+now.Format(backupStamp))
	for i := 0; ; i++ {
		name := base
		if i > 0 {
			name = fmt.Sprintf("%s-%d", base, i)
		}
		f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, FileMode)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("creating %s: %w", name, err)
		}
		if _, err := f.Write(data); err != nil {
			f.Close()
			return "", fmt.Errorf("writing %s: %w", name, err)
		}
		if err := f.Close(); err != nil {
			return "", fmt.Errorf("closing %s: %w", name, err)
		}
		return name, nil
	}
}
