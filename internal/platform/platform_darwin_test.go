package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
