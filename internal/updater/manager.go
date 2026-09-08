package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type Status struct {
	DownloadDir        string    `json:"download_dir"`
	CustomDownloadDir  string    `json:"custom_download_dir"`
	DefaultDownloadDir string    `json:"default_download_dir"`
	LastDownload       string    `json:"last_download,omitempty"`
	CanOpenDownloadDir bool      `json:"can_open_download_dir"`
	Current            string    `json:"current_version"`
	Latest             string    `json:"latest_version"`
	ReleaseURL         string    `json:"release_url"`
	AutoCheck          bool      `json:"auto_check"`
	Available          bool      `json:"available"`
	CanInstall         bool      `json:"can_install"`
	Checking           bool      `json:"checking"`
	CheckedAt          time.Time `json:"checked_at"`
	Phase              string    `json:"phase"`
	Error              string    `json:"error,omitempty"`
	Target             string    `json:"target_version,omitempty"`
}
type Result struct {
	PID     int       `json:"pid"`
	Phase   string    `json:"phase"`
	Target  string    `json:"target_version"`
	Error   string    `json:"error,omitempty"`
	Updated time.Time `json:"updated_at"`
}
type Manager struct {
	mu                       sync.Mutex
	stateDir, version, asset string
	client                   *http.Client
	endpoint                 string
	apply                    func(string, string, string) error
	status                   Status
	release                  Release
	openDirectory            func(string) error
}
type saved struct {
	DownloadDir  string    `json:"download_dir,omitempty"`
	LastDownload string    `json:"last_download,omitempty"`
	AutoCheck    bool      `json:"auto_check"`
	CheckedAt    time.Time `json:"checked_at"`
	Release      Release   `json:"release"`
}

func New(stateDir, version, goos, arch string, apply func(string, string, string) error) *Manager {
	m := &Manager{stateDir: stateDir, version: version, asset: AssetName(goos, arch), client: HTTPClient(), endpoint: LatestURL, apply: apply}
	s := saved{AutoCheck: true}
	if data, err := os.ReadFile(m.file()); err == nil {
		_ = json.Unmarshal(data, &s)
	}
	m.release = s.Release
	m.status = Status{Current: version, AutoCheck: s.AutoCheck, CheckedAt: s.CheckedAt, Phase: "idle", CanInstall: apply != nil && m.asset != ""}
	m.status.DefaultDownloadDir = defaultDownloadDirectory(stateDir)
	m.status.CustomDownloadDir = s.DownloadDir
	m.status.DownloadDir = m.effectiveDownloadDir(s.DownloadDir)
	m.status.LastDownload = s.LastDownload
	m.setRelease()
	return m
}
func (m *Manager) file() string { return filepath.Join(m.stateDir, ".codex-usage-updates.json") }
func (m *Manager) setRelease() {
	m.status.Latest = strings.TrimPrefix(m.release.Tag, "v")
	m.status.Available = !m.release.Draft && !m.release.Prerelease && Newer(m.release.Tag, m.version)
	m.status.ReleaseURL = RepositoryURL + "/releases"
	if stableTag.MatchString(m.release.Tag) {
		m.status.ReleaseURL = RepositoryURL + "/releases/tag/" + m.release.Tag
	}
}
func WriteJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".update-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	// Readers and antivirus scanners can briefly deny replacement on Windows.
	deadline := time.Now().Add(2 * time.Second)
	for {
		err = os.Rename(f.Name(), path)
		if err == nil || runtime.GOOS != "windows" || !os.IsPermission(err) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(20 * time.Millisecond)
	}
}
func (m *Manager) save() error {
	return WriteJSON(m.file(), saved{AutoCheck: m.status.AutoCheck, CheckedAt: m.status.CheckedAt, Release: m.release, DownloadDir: m.status.CustomDownloadDir, LastDownload: m.status.LastDownload})
}
func ResultPath(dir string) string { return filepath.Join(dir, ".codex-usage-update-result.json") }
func WriteResult(dir string, r Result) error {
	r.PID = os.Getpid()
	r.Updated = time.Now().UTC()
	return WriteJSON(ResultPath(dir), r)
}
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.status
	var result Result
	if data, err := os.ReadFile(ResultPath(m.stateDir)); err == nil && json.Unmarshal(data, &result) == nil {
		if result.Target != "" {
			s.Phase, s.Target = result.Phase, result.Target
			if result.Error != "" {
				s.Error = result.Error
			}
			if busy(s.Phase) && !processAlive(result.PID) {
				s.Phase = "failed"
				s.Error = "The update was interrupted; check the running version before retrying."
			}
		}
	}
	m.status.Phase = s.Phase
	return s
}
func busy(phase string) bool { return phase == "downloading" || phase == "installing" }
func (m *Manager) SetAutoCheck(enabled bool) error {
	return m.SetPreferences(&enabled, nil)
}
func (m *Manager) Check(ctx context.Context) error {
	m.Status()
	m.mu.Lock()
	if m.status.Checking || busy(m.status.Phase) {
		m.mu.Unlock()
		return fmt.Errorf("update operation already running")
	}
	m.status.Checking = true
	m.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	r, err := fetchRelease(ctx, m.client, m.endpoint, m.asset)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status.Checking = false
	m.status.CheckedAt = time.Now().UTC()
	if err != nil {
		m.status.Error = err.Error()
	} else {
		m.release = r
		m.setRelease()
		m.status.Error = ""
	}
	if saveErr := m.save(); err == nil {
		err = saveErr
	}
	return err
}
func (m *Manager) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		s := m.Status()
		if s.AutoCheck && !s.Checking && !busy(s.Phase) && (s.CheckedAt.IsZero() || time.Since(s.CheckedAt) >= 6*time.Hour) {
			_ = m.Check(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// No downloads occur until the UI submits an explicitly selected version.
func (m *Manager) Install(version string) error {
	status := m.Status()
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.status.CanInstall {
		return fmt.Errorf("run the installed application to update")
	}
	if busy(m.status.Phase) || busy(status.Phase) || m.status.Checking {
		return fmt.Errorf("update operation already running")
	}
	if !m.status.Available || version != m.status.Latest {
		return fmt.Errorf("check and select an available version first")
	}
	if _, _, err := m.release.assets(m.asset); err != nil {
		return err
	}
	m.status.Phase = "downloading"
	m.status.Error = ""
	m.status.Target = version
	if err := WriteResult(m.stateDir, Result{Phase: "downloading", Target: version}); err != nil {
		m.status.Phase = "failed"
		return err
	}
	r := m.release
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
		defer cancel()
		err := m.prepare(ctx, r)
		if err != nil {
			m.mu.Lock()
			m.status.Phase = "failed"
			m.status.Error = err.Error()
			m.mu.Unlock()
			_ = WriteResult(m.stateDir, Result{Phase: "failed", Target: version, Error: err.Error()})
		}
	}()
	return nil
}
func (m *Manager) prepare(ctx context.Context, r Release) error {
	m.mu.Lock()
	downloadDir := m.status.DownloadDir
	m.mu.Unlock()
	if err := ensureDownloadDirectory(downloadDir); err != nil {
		return err
	}
	downloadRun, err := os.MkdirTemp(downloadDir, "codex-usage-"+r.Tag+"-")
	if err != nil {
		return err
	}
	downloadPath := filepath.Join(downloadRun, m.asset)
	digest, err := download(ctx, m.client, r, m.asset, downloadPath)
	if err != nil {
		_ = os.Remove(downloadRun)
		return err
	}
	m.mu.Lock()
	m.status.LastDownload = downloadPath
	err = m.save()
	m.mu.Unlock()
	if err != nil {
		return err
	}
	root := filepath.Join(m.stateDir, ".codex-usage-updates")
	if err := os.MkdirAll(root, 0700); err != nil {
		return err
	}
	dir, err := os.MkdirTemp(root, "run-")
	if err != nil {
		return err
	}
	name := "candidate"
	if strings.HasSuffix(m.asset, ".exe") {
		name += ".exe"
	}
	path := filepath.Join(dir, name)
	err = stageDownload(downloadPath, path, digest)
	if err != nil {
		return err
	}
	version := strings.TrimPrefix(r.Tag, "v")
	if err = WriteResult(m.stateDir, Result{Phase: "installing", Target: version}); err != nil {
		return err
	}
	m.mu.Lock()
	m.status.Phase = "installing"
	m.mu.Unlock()
	return m.apply(path, version, digest)
}
