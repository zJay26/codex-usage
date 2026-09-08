package platform

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const launchAgentLabel = "com.zjay.codex-usage"

func launchDomain() string { return "gui/" + strconv.Itoa(os.Getuid()) }
func launchAgentPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist"), nil
}
func plistString(value string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(value))
	return "<string>" + b.String() + "</string>"
}
func launchPlist(label, executable, stateDir string, args []string, keepAlive bool) (string, error) {
	if !filepath.IsAbs(executable) || !filepath.IsAbs(stateDir) || strings.ContainsAny(executable+stateDir, "\x00\r\n") {
		return "", fmt.Errorf("invalid launch agent path")
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>Label</key>`)
	b.WriteString(plistString(label) + "<key>ProgramArguments</key><array>" + plistString(executable))
	for _, arg := range args {
		b.WriteString(plistString(arg))
	}
	b.WriteString("</array><key>EnvironmentVariables</key><dict><key>CODEX_USAGE_HOME</key>" + plistString(stateDir))
	if keepAlive {
		b.WriteString("<key>CODEX_USAGE_LAUNCHD</key><string>1</string>")
	}
	for _, name := range []string{"CODEX_HOME", "CODEX_USAGE_TIMEZONE", "TZ"} {
		if value := os.Getenv(name); value != "" {
			b.WriteString("<key>" + name + "</key>" + plistString(value))
		}
	}
	b.WriteString("</dict><key>RunAtLoad</key><true/>")
	if keepAlive {
		b.WriteString("<key>KeepAlive</key><true/><key>ThrottleInterval</key><integer>5</integer>")
	}
	b.WriteString("<key>ProcessType</key><string>Background</string><key>ExitTimeOut</key><integer>25</integer></dict></plist>")
	return b.String(), nil
}
func launchctl(args ...string) error {
	output, err := exec.Command("/bin/launchctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func InstallService(executable, stateDir string) (ServiceResult, error) {
	path, err := launchAgentPath()
	if err != nil {
		return ServiceResult{}, err
	}
	plist, err := launchPlist(launchAgentLabel, executable, stateDir, []string{"daemon"}, true)
	if err != nil {
		return ServiceResult{}, err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return ServiceResult{}, err
	}
	// Unload the previous job before replacing it; bootout waits for launchd to
	// remove its keep-alive policy, so replacement cannot start a second daemon.
	if err = StopForUpdate(executable, stateDir, true); err != nil {
		return ServiceResult{}, err
	}
	if err = os.WriteFile(path, []byte(plist), 0600); err != nil {
		return ServiceResult{}, err
	}
	if err = launchctl("bootstrap", launchDomain(), path); err != nil {
		return ServiceResult{Installed: true, Detail: path, Warning: "LaunchAgent 已保存。请在 macOS 图形登录会话中重试 install；也可使用 serve 前台运行。"}, err
	}
	return ServiceResult{Installed: true, Started: true, Detail: path}, nil
}
func UninstallService(executable, stateDir string) error {
	if err := StopForUpdate(executable, stateDir, true); err != nil {
		return err
	}
	path, err := launchAgentPath()
	if err != nil {
		return err
	}
	if err = os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	_ = os.Remove(filepath.Join(stateDir, "codex-usage.pid"))
	return nil
}
func LockDown(path string) error  { return os.Chmod(path, 0700) }
func SetPrivateUmask()            { syscall.Umask(0077) }
func HideConsole()                {}
func HasGUI() bool                { return os.Getenv("SSH_CONNECTION") == "" }
func OpenURL(rawURL string) error { return exec.Command("/usr/bin/open", rawURL).Start() }
func StartDetached(executable string, args ...string) error {
	command := exec.Command(executable, args...)
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}
func StopPreviousService(previous PreviousService) error {
	pid, err := readPIDFile(previous.PIDPath)
	if err == nil {
		if err = stopDarwinProcess(pid, previous.Executable); err != nil {
			return err
		}
	}
	if err = os.Remove(previous.PIDPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
func RemovePreviousExecutable(previous PreviousService) error {
	return removePreviousExecutable(previous)
}
func RemoveInstalledExecutable(executable, stateDir string, purge bool) error {
	if filepath.Base(executable) != "codex-usage" {
		return fmt.Errorf("refusing to remove a different executable")
	}
	if purge {
		if err := ValidatePurgeStateDir(stateDir); err != nil {
			return err
		}
	}
	if err := os.Remove(executable); err != nil && !os.IsNotExist(err) {
		return err
	}
	if purge {
		return os.RemoveAll(stateDir)
	}
	return nil
}
func stopDarwinProcess(pid int, executable string) error {
	if pid <= 0 || pid == os.Getpid() {
		return nil
	}
	matches, live, err := darwinProcessIdentity(pid, executable)
	if err != nil || !live {
		return err
	}
	if !matches {
		return fmt.Errorf("service PID does not match installed application")
	}
	if err = syscall.Kill(pid, syscall.SIGTERM); err != nil && err != syscall.ESRCH {
		return err
	}
	return waitDarwinProcessExit(pid, executable)
}

func darwinProcessIdentity(pid int, executable string) (matches, live bool, err error) {
	output, err := exec.Command("/bin/ps", "-p", strconv.Itoa(pid), "-o", "stat=", "-o", "comm=").Output()
	if err != nil {
		if syscall.Kill(pid, 0) == syscall.ESRCH {
			return false, false, nil
		}
		return false, false, err
	}
	fields := strings.Fields(string(output))
	if len(fields) < 2 || strings.HasPrefix(fields[0], "Z") {
		// An exited launchd child can remain briefly as a zombie. Its command
		// path is no longer reliable, and it must not be signalled again.
		return false, false, nil
	}
	actual := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(output)), fields[0]))
	expected, _ := filepath.EvalSymlinks(executable)
	return actual == executable || (expected != "" && actual == expected), true, nil
}

// Call only after the matching process was signalled or its launchd job was
// booted out. A changed identity means the original PID has been reaped/reused;
// never send a signal to the new occupant of that PID.
func waitDarwinProcessExit(pid int, executable string) error {
	if pid <= 0 || pid == os.Getpid() {
		return nil
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		matches, live, err := darwinProcessIdentity(pid, executable)
		if err != nil {
			return err
		}
		if !live || !matches {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("application did not stop; operation cancelled")
}
