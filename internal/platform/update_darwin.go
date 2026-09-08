package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func ManagedUpdateService() bool { return os.Getenv("CODEX_USAGE_LAUNCHD") == "1" }
func StartUpdateHelper(executable, job, stateDir string, managed bool) error {
	if !managed {
		return StartDetached(executable, "_apply-update", job)
	}
	label := fmt.Sprintf("%s.update-%d", launchAgentLabel, time.Now().UnixNano())
	content, err := launchPlist(label, executable, stateDir, []string{"_apply-update", job}, false)
	if err != nil {
		return err
	}
	path := filepath.Join(filepath.Dir(job), "helper.plist")
	if err = os.WriteFile(path, []byte(content), 0600); err != nil {
		return err
	}
	return launchctl("bootstrap", launchDomain(), path)
}
func StopForUpdate(executable, stateDir string, managed bool) error {
	pid, _ := ReadPID(stateDir)
	if managed {
		target := launchDomain() + "/" + launchAgentLabel
		// A missing service is normal for the first install or a rollback retry.
		if launchctl("print", target) == nil {
			if err := launchctl("bootout", target); err != nil {
				return err
			}
			return waitDarwinProcessExit(pid, executable)
		}
	}
	return stopDarwinProcess(pid, executable)
}
func StartAfterUpdate(executable string, managed bool) error {
	if !managed {
		return StartDetached(executable, "daemon")
	}
	path, err := launchAgentPath()
	if err != nil {
		return err
	}
	return launchctl("bootstrap", launchDomain(), path)
}
