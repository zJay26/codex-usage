package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestLaunchAgentEscapesPathsAndSeparatesUpdateHelper(t *testing.T) {
	t.Setenv("CODEX_HOME", "/tmp/codex & <source>")
	for _, managed := range []bool{true, false} {
		body, err := launchPlist("test.label", "/tmp/a & b/codex-usage", "/tmp/state <test>", []string{"daemon"}, managed)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "agent.plist")
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if output, err := exec.Command("/usr/bin/plutil", "-lint", path).CombinedOutput(); err != nil {
			t.Fatalf("plist invalid: %v %s", err, output)
		}
		if strings.Contains(body, "<key>KeepAlive</key>") != managed {
			t.Fatal("helper inherited keep-alive")
		}
		if !strings.Contains(body, "a &amp; b") || !strings.Contains(body, "&lt;source&gt;") {
			t.Fatal("paths were not escaped")
		}
	}
	if _, err := launchPlist("test", "relative", "/tmp/state", nil, true); err == nil {
		t.Fatal("relative executable accepted")
	}
}

func TestDarwinStopRejectsAnotherExecutable(t *testing.T) {
	cmd := exec.Command("/bin/sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	if err := stopDarwinProcess(cmd.Process.Pid, "/tmp/codex-usage"); err == nil {
		t.Fatal("unrelated process was not rejected")
	}
}

func TestDarwinStopAcceptsExitedChildBeforeReaping(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Wait() })
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, live, err := darwinProcessIdentity(cmd.Process.Pid, "/bin/sh")
		if err != nil {
			t.Fatal(err)
		}
		if !live {
			if err := stopDarwinProcess(cmd.Process.Pid, "/bin/sh"); err != nil {
				t.Fatal(err)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("child did not exit")
}

func TestDarwinWaitDoesNotSignalReusedPID(t *testing.T) {
	cmd := exec.Command("/bin/sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	if err := waitDarwinProcessExit(cmd.Process.Pid, "/tmp/previous/codex-usage"); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(cmd.Process.Pid, 0); err != nil {
		t.Fatal("replacement process was signalled", err)
	}
}
