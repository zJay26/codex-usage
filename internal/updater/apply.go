package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Transaction runs in a separate process, after the user approved this version.
// Stop must wait for the application to exit before any database files are copied.
type Transaction struct {
	Directory, Candidate, Executable, Digest string
	DataFiles                                []string
	Stop, Start, Healthy                     func() error
	Copy                                     func(string, string) error
}

func Verify(path, digest string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != digest {
		return fmt.Errorf("update checksum mismatch")
	}
	return nil
}

func (t Transaction) Run() (string, error) {
	if err := Verify(t.Candidate, t.Digest); err != nil {
		return "failed", err
	}
	if err := t.Stop(); err != nil {
		return "failed", fmt.Errorf("stop application: %w", err)
	}
	backupExe := filepath.Join(t.Directory, "previous-program")
	restart := func(err error) (string, error) {
		if startErr := t.Start(); startErr != nil {
			return "failed", fmt.Errorf("%v; restart previous version: %w", err, startErr)
		}
		return "failed", err
	}
	if err := t.Copy(t.Executable, backupExe); err != nil {
		return restart(err)
	}
	present := make([]bool, len(t.DataFiles))
	for i, path := range t.DataFiles {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return restart(err)
		}
		if err := t.Copy(path, filepath.Join(t.Directory, fmt.Sprintf("backup-%d", i))); err != nil {
			return restart(err)
		}
		present[i] = true
	}
	err := t.Copy(t.Candidate, t.Executable)
	if err == nil {
		err = t.Start()
	}
	if err == nil {
		err = t.Healthy()
	}
	if err == nil {
		return "updated", nil
	}
	// The new version may have migrated the database; restore both its main file
	// and WAL from the same stopped-process snapshot before starting the old binary.
	if stopErr := t.Stop(); stopErr != nil {
		return "failed", fmt.Errorf("%v; cannot stop failed update: %w", err, stopErr)
	}
	if restoreErr := t.Copy(backupExe, t.Executable); restoreErr != nil {
		return "failed", fmt.Errorf("%v; restore executable: %w", err, restoreErr)
	}
	for i, path := range t.DataFiles {
		if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
			return "failed", fmt.Errorf("%v; restore data: %w", err, removeErr)
		}
		if present[i] {
			if restoreErr := t.Copy(filepath.Join(t.Directory, fmt.Sprintf("backup-%d", i)), path); restoreErr != nil {
				return "failed", fmt.Errorf("%v; restore data: %w", err, restoreErr)
			}
		}
	}
	if startErr := t.Start(); startErr != nil {
		return "failed", fmt.Errorf("%v; restart previous version: %w", err, startErr)
	}
	return "rolled_back", err
}
