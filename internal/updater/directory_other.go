//go:build !windows

package updater

import (
	"os"
	"path/filepath"
)

func userDownloadsDirectory() (string, error) {
	home, err := os.UserHomeDir()
	return filepath.Join(home, "Downloads"), err
}
