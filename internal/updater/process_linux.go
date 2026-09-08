package updater

import (
	"fmt"
	"os"
)

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	_, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	return !os.IsNotExist(err)
}
