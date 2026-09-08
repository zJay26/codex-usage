package updater

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDownloadDirectoryPersistsValidatesAndOpens(t *testing.T) {
	m, _ := testManager(t, false, nil)
	want := filepath.Join(t.TempDir(), "updates with spaces", "下载")
	off := false
	if err := m.SetPreferences(&off, &want); err != nil {
		t.Fatal(err)
	}
	n := New(m.stateDir, "2.4.1", "windows", "amd64", nil)
	if s := n.Status(); s.DownloadDir != want || s.CustomDownloadDir != want || s.AutoCheck {
		t.Fatal(s)
	}
	opened := ""
	n.SetDirectoryOpener(func(path string) error { opened = path; return nil })
	if err := n.OpenDownloadDirectory(); err != nil || opened != want {
		t.Fatal(opened, err)
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("keep me"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"relative/path", file, want + "\nwrong"} {
		on := true
		if err := n.SetPreferences(&on, &bad); err == nil {
			t.Fatal("accepted invalid directory", bad)
		}
		if s := n.Status(); s.DownloadDir != want || s.AutoCheck {
			t.Fatal("partial preferences saved", s)
		}
	}
	if err := WriteResult(n.stateDir, Result{Phase: "installing", Target: "2.5.0"}); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "other")
	if err := n.SetPreferences(nil, &other); err == nil {
		t.Fatal("changed directory during installation")
	}
	if err := WriteResult(n.stateDir, Result{Phase: "failed", Target: "2.5.0"}); err != nil {
		t.Fatal(err)
	}
	// Use an isolated default for this test; never create directories in the real Downloads.
	n.status.DefaultDownloadDir = filepath.Join(t.TempDir(), "default")
	empty := ""
	if err := n.SetPreferences(nil, &empty); err != nil {
		t.Fatal(err)
	}
	if s := n.Status(); s.DownloadDir != s.DefaultDownloadDir || s.CustomDownloadDir != "" {
		t.Fatal(s)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatal("old download directory removed", err)
	}
}

func TestDownloadUsesSelectedDirectoryAndPrivateStaging(t *testing.T) {
	applied := make(chan string, 1)
	m, _ := testManager(t, false, func(path, version, digest string) error {
		if err := Verify(path, digest); err != nil {
			return err
		}
		applied <- path
		return nil
	})
	if err := m.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Install("2.5.0"); err != nil {
		t.Fatal(err)
	}
	until(t, func() bool { return len(applied) > 0 })
	private := <-applied
	if !strings.HasPrefix(private, filepath.Join(m.stateDir, ".codex-usage-updates")+string(os.PathSeparator)) {
		t.Fatal("helper staging escaped state directory", private)
	}
	s := m.Status()
	if filepath.Dir(filepath.Dir(s.LastDownload)) != s.DownloadDir || filepath.Base(s.LastDownload) != m.asset || !strings.HasPrefix(filepath.Base(filepath.Dir(s.LastDownload)), "codex-usage-v2.5.0-") {
		t.Fatal(s)
	}
	entries, err := os.ReadDir(filepath.Dir(s.LastDownload))
	if err != nil || len(entries) != 1 {
		t.Fatal("backup/helper leaked into download folder", entries, err)
	}
	if New(m.stateDir, "2.5.0", "windows", "amd64", nil).Status().LastDownload != s.LastDownload {
		t.Fatal("last download path not persisted")
	}
	if err := os.WriteFile(s.LastDownload, []byte("tampered public copy"), 0600); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(private)
	if err != nil || string(content) != "test binary contents" {
		t.Fatal("public changes affected staged installer", err)
	}
	if err := stageDownload(s.LastDownload, filepath.Join(t.TempDir(), "staged"), strings.Repeat("0", 64)); err == nil {
		t.Fatal("modified download passed staging check")
	}
}
