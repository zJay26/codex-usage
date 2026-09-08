package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/zJay26/codex-usage/internal/config"
	"github.com/zJay26/codex-usage/internal/platform"
	"github.com/zJay26/codex-usage/internal/updater"
)

func TestUpdateInstallationPathSurvivesLauncherEnvironment(t *testing.T) {
	dir := t.TempDir()
	paths := config.Paths{StateDir: dir, InstalledEXE: filepath.Join(dir, "original", "codex-usage.exe")}
	if err := recordInstallation(paths); err != nil {
		t.Fatal(err)
	}
	fromLauncher := paths
	fromLauncher.InstalledEXE = filepath.Join(dir, "bin", "codex-usage.exe")
	if got := updatePaths(fromLauncher); got.InstalledEXE != paths.InstalledEXE {
		t.Fatal(got)
	}
}

// Exercise real executable replacement and database reopening without touching
// login startup entries or any real Codex Home. CI runs this on Windows/Linux.
func TestUpdateRealServicePreservesStatistics(t *testing.T) {
	if testing.Short() {
		t.Skip("builds two application versions")
	}
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	home := filepath.Join(root, "codex")
	t.Setenv("CODEX_USAGE_HOME", stateDir)
	t.Setenv("CODEX_HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "sessions"), 0700); err != nil {
		t.Fatal(err)
	}
	paths, err := config.ResolvePaths()
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	cfg := config.Default()
	cfg.Port = port
	if err = config.Save(paths, cfg); err != nil {
		t.Fatal(err)
	}
	if err = config.EnsureStateMarker(paths); err != nil {
		t.Fatal(err)
	}
	if err = recordInstallation(paths); err != nil {
		t.Fatal(err)
	}
	if err = updater.WriteJSON(filepath.Join(stateDir, ".codex-usage-updates.json"), map[string]bool{"auto_check": false}); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Format(time.RFC3339)
	fixture := fmt.Sprintf(`{"timestamp":%q,"type":"session_meta","payload":{"id":"update-test","cwd":"/fixture"}}
{"timestamp":%q,"type":"turn_context","payload":{"turn_id":"turn-update","model":"gpt-5.4","service_tier":"priority"}}
{"timestamp":%q,"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":80,"output_tokens":20,"total_tokens":100},"last_token_usage":{"input_tokens":80,"output_tokens":20,"total_tokens":100}}}}
`, at, at, at)
	if err = os.WriteFile(filepath.Join(home, "sessions", "rollout-update.jsonl"), []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(stateDir, ".codex-usage-updates", "run-integration")
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(dir, "candidate")
	helper := filepath.Join(dir, "update-helper")
	if runtime.GOOS == "windows" {
		candidate += ".exe"
		helper += ".exe"
	}
	build := func(out, version string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-ldflags", "-X github.com/zJay26/codex-usage/internal/app.Version="+version, "-o", out, "../../cmd/codex-usage")
		if data, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build: %v %s", err, data)
		}
	}
	build(paths.InstalledEXE, "2.4.1")
	build(candidate, "2.5.0")
	if err = copyExecutable(paths.InstalledEXE, helper); err != nil {
		t.Fatal(err)
	}
	old := exec.Command(paths.InstalledEXE, "serve")
	if err = old.Start(); err != nil {
		t.Fatal(err)
	}
	oldDone := make(chan error, 1)
	go func() { oldDone <- old.Wait() }()
	t.Cleanup(func() {
		_ = platform.StopForUpdate(paths.InstalledEXE, stateDir, false)
		select {
		case <-oldDone:
		case <-time.After(5 * time.Second):
		}
	})
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	if err = waitUpdateHealth(base+"/healthz", "2.4.1"); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 3 * time.Second}
	checkTotal := func() bool {
		res, err := client.Get(base + "/api/v1/summary?since=all")
		if err != nil {
			return false
		}
		defer res.Body.Close()
		var body struct {
			Total int `json:"grand_total"`
		}
		return json.NewDecoder(res.Body).Decode(&body) == nil && body.Total == 100
	}
	deadline := time.Now().Add(10 * time.Second)
	for !checkTotal() {
		if time.Now().After(deadline) {
			t.Fatal("fixture not scanned")
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Removing the source is intentional: updates must preserve the existing
	// ledger, not silently rebuild it from only the files still present.
	if err = os.Remove(filepath.Join(home, "sessions", "rollout-update.jsonl")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(candidate)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	jobPath := filepath.Join(dir, "job.json")
	if err = updater.WriteJSON(jobPath, updateJob{Version: "2.5.0", Digest: hex.EncodeToString(hash[:])}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if output, err := exec.CommandContext(ctx, helper, "_apply-update", jobPath).CombinedOutput(); err != nil {
		t.Fatalf("helper: %v %s", err, output)
	}
	if err = waitUpdateHealth(base+"/healthz", "2.5.0"); err != nil {
		t.Fatal(err)
	}
	if !checkTotal() {
		t.Fatal("statistics changed across update")
	}
	var result updater.Result
	data, _ = os.ReadFile(updater.ResultPath(stateDir))
	if err = json.Unmarshal(data, &result); err != nil || result.Phase != "updated" {
		t.Fatalf("%s %v", data, err)
	}
	res, err := client.Get(base + "/api/v1/updates")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var status updater.Status
	if err = json.NewDecoder(res.Body).Decode(&status); err != nil || !status.CanInstall || status.AutoCheck {
		t.Fatalf("%+v %v", status, err)
	}
	if _, err = os.Stat(filepath.Join(dir, "previous-program")); err != nil {
		t.Fatal("no backup", err)
	}
}
