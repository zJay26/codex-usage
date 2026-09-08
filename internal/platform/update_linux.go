package platform

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

func ManagedUpdateService() bool {
	data, _ := os.ReadFile("/proc/self/cgroup")
	return strings.Contains(string(data), "/codex-usage.service")
}
func StartUpdateHelper(executable, job, stateDir string, managed bool) error {
	if !managed {
		return StartDetached(executable, "_apply-update", job)
	}
	// A detached child remains in the original unit's cgroup. A transient unit
	// keeps the helper alive when codex-usage.service is stopped.
	args := []string{"--user", "--collect", "--unit", fmt.Sprintf("codex-usage-update-%d", time.Now().UnixNano()), "--setenv=CODEX_USAGE_HOME=" + stateDir}
	for _, name := range []string{"CODEX_HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME"} {
		if value := os.Getenv(name); value != "" {
			args = append(args, "--setenv="+name+"="+value)
		}
	}
	args = append(args, "--", executable, "_apply-update", job)
	output, err := exec.Command("systemd-run", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("start update helper: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
func StopForUpdate(executable, stateDir string, managed bool) error {
	pid, _ := ReadPID(stateDir)
	if managed {
		if output, err := exec.Command("systemctl", "--user", "stop", "codex-usage.service").CombinedOutput(); err != nil {
			return fmt.Errorf("stop service: %w: %s", err, output)
		}
	}
	if pid <= 0 || pid == os.Getpid() {
		return nil
	}
	procPath := fmt.Sprintf("/proc/%d/exe", pid)
	actual, err := os.Readlink(procPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if actual != executable {
		return fmt.Errorf("service process does not match installed application")
	}
	if err = syscall.Kill(pid, syscall.SIGTERM); err != nil && err != syscall.ESRCH {
		return err
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if _, err = os.Readlink(procPath); os.IsNotExist(err) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("application did not stop; update cancelled")
}
func StartAfterUpdate(executable string, managed bool) error {
	if !managed {
		return StartDetached(executable, "daemon")
	}
	output, err := exec.Command("systemctl", "--user", "start", "codex-usage.service").CombinedOutput()
	if err != nil {
		return fmt.Errorf("start service: %w: %s", err, output)
	}
	return nil
}
