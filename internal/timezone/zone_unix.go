//go:build !windows

package timezone

import (
	"os"
	"path/filepath"
	"strings"
)

func systemZone() string {
	if path, err := filepath.EvalSymlinks("/etc/localtime"); err == nil {
		if _, name, ok := strings.Cut(path, "/zoneinfo/"); ok {
			return name
		}
	}
	if data, err := os.ReadFile("/etc/timezone"); err == nil {
		return strings.TrimSpace(string(data))
	}
	return "UTC"
}
