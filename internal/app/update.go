package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/zJay26/codex-usage/internal/config"
	"github.com/zJay26/codex-usage/internal/platform"
	"github.com/zJay26/codex-usage/internal/updater"
)

type updateJob struct {
	Version string `json:"version"`
	Digest  string `json:"digest"`
	Managed bool   `json:"managed"`
}

func newUpdater(paths config.Paths) *updater.Manager {
	paths = updatePaths(paths)
	executable, _ := os.Executable()
	current, err1 := os.Stat(executable)
	installed, err2 := os.Stat(paths.InstalledEXE)
	var apply func(string, string, string) error
	if err1 == nil && err2 == nil && os.SameFile(current, installed) {
		apply = func(candidate, version, digest string) error {
			dir := filepath.Dir(candidate)
			helper := filepath.Join(dir, "update-helper")
			if runtime.GOOS == "windows" {
				helper += ".exe"
			}
			if err := copyExecutable(executable, helper); err != nil {
				return err
			}
			job := updateJob{Version: version, Digest: digest, Managed: platform.ManagedUpdateService()}
			path := filepath.Join(dir, "job.json")
			if err := updater.WriteJSON(path, job); err != nil {
				return err
			}
			return platform.StartUpdateHelper(helper, path, paths.StateDir, job.Managed)
		}
	}
	m := updater.New(paths.StateDir, Version, runtime.GOOS, runtime.GOARCH, apply)
	if platform.HasGUI() {
		m.SetDirectoryOpener(platform.OpenDirectory)
	}
	return m
}

func (c CLI) applyUpdate(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("expected one update job")
	}
	paths, err := config.ResolvePaths()
	if err != nil {
		return err
	}
	paths = updatePaths(paths)
	path, err := filepath.Abs(args[0])
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	root := filepath.Join(paths.StateDir, ".codex-usage-updates")
	helper, _ := os.Executable()
	if filepath.Base(path) != "job.json" || filepath.Dir(dir) != root || !strings.HasPrefix(filepath.Base(dir), "run-") || filepath.Dir(helper) != dir {
		return fmt.Errorf("invalid update job location")
	}
	// Reject symlinked directories before touching the installed binary or data.
	for _, p := range []string{root, dir, path} {
		info, e := os.Lstat(p)
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlinked update job")
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var job updateJob
	if err = json.Unmarshal(data, &job); err != nil {
		return err
	}
	if !updater.Newer(job.Version, Version) {
		return fmt.Errorf("update must select a newer stable version")
	}
	defer func() { _ = os.Remove(path) }() // A completed job cannot be replayed.
	if err = updater.WriteResult(paths.StateDir, updater.Result{Phase: "installing", Target: job.Version}); err != nil {
		return err
	}
	phase, runErr := runUpdate(paths, dir, job)
	result := updater.Result{Phase: phase, Target: job.Version}
	if runErr != nil {
		result.Error = runErr.Error()
	}
	if err = updater.WriteResult(paths.StateDir, result); err != nil {
		return err
	}
	return runErr
}

// Login launchers set CODEX_USAGE_HOME even for the default installation.
// Remember the actual install destination so that it is stable across restarts.
func recordInstallation(paths config.Paths) error {
	return updater.WriteJSON(filepath.Join(paths.StateDir, ".codex-usage-install.json"), map[string]string{"executable": paths.InstalledEXE})
}
func updatePaths(paths config.Paths) config.Paths {
	var record struct {
		Executable string `json:"executable"`
	}
	data, err := os.ReadFile(filepath.Join(paths.StateDir, ".codex-usage-install.json"))
	if err == nil && json.Unmarshal(data, &record) == nil && filepath.IsAbs(record.Executable) && filepath.Base(record.Executable) == filepath.Base(paths.InstalledEXE) {
		paths.InstalledEXE = filepath.Clean(record.Executable)
		paths.InstallDir = filepath.Dir(paths.InstalledEXE)
	}
	return paths
}

func runUpdate(paths config.Paths, dir string, job updateJob) (string, error) {
	candidate := filepath.Join(dir, "candidate")
	if runtime.GOOS == "windows" {
		candidate += ".exe"
	}
	if err := updater.Verify(candidate, job.Digest); err != nil {
		return "failed", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, candidate, "--version").Output()
	if err != nil || !strings.HasPrefix(string(output), "codex-usage "+job.Version+" ") {
		return "failed", fmt.Errorf("downloaded application cannot run or has an unexpected version")
	}
	cfg, err := config.Load(paths)
	if err != nil {
		return "failed", err
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/healthz", cfg.Port)
	t := updater.Transaction{
		Directory: dir, Candidate: candidate, Executable: paths.InstalledEXE, Digest: job.Digest,
		DataFiles: []string{paths.ConfigPath, paths.Database, paths.Database + "-wal", paths.Database + "-shm", paths.Database + "-journal"},
		Copy:      copyUpdateFile,
		Stop:      func() error { return platform.StopForUpdate(paths.InstalledEXE, paths.StateDir, job.Managed) },
		Start:     func() error { return platform.StartAfterUpdate(paths.InstalledEXE, job.Managed) },
		Healthy:   func() error { return waitUpdateHealth(url, job.Version) },
	}
	phase, err := t.Run()
	if phase == "rolled_back" {
		if healthErr := waitUpdateHealth(url, Version); healthErr != nil {
			return "failed", fmt.Errorf("%v; previous version did not recover: %w", err, healthErr)
		}
	}
	return phase, err
}

func copyUpdateFile(source, destination string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	if err = copyExecutable(source, destination); err != nil {
		return err
	}
	return os.Chmod(destination, info.Mode().Perm())
}

func waitUpdateHealth(url, version string) error {
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		res, err := client.Get(url)
		if err == nil {
			var body struct {
				OK      bool   `json:"ok"`
				Version string `json:"version"`
			}
			err = json.NewDecoder(res.Body).Decode(&body)
			res.Body.Close()
			if err == nil && res.StatusCode == 200 && body.OK && body.Version == version {
				return nil
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("version %s did not become healthy within 45 seconds", version)
}
