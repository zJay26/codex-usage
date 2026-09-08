package platform

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func OpenDirectory(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("expected an absolute directory")
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("expected a directory")
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer.exe", path)
	case "darwin":
		cmd = exec.Command("open", path)
	case "linux":
		if !HasGUI() {
			return fmt.Errorf("no desktop session available; copy the directory path instead")
		}
		cmd = exec.Command("xdg-open", path)
	default:
		return fmt.Errorf("opening directories is unsupported")
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
