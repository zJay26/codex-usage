package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func fixture() (Release, string, string) {
	body := "test binary contents"
	h := sha256.Sum256([]byte(body))
	digest := hex.EncodeToString(h[:])
	name := AssetName("windows", "amd64")
	sums := digest + "  " + name + "\n"
	asset := func(name string, size int64) Asset {
		return Asset{Name: name, Size: size, URL: RepositoryURL + "/releases/download/v2.5.0/" + name}
	}
	r := Release{Tag: "v2.5.0", Assets: []Asset{asset(name, int64(len(body))), asset("SHA256SUMS", int64(len(sums)))}}
	return r, body, sums
}
func testManager(t *testing.T, corrupt bool, apply func(string, string, string) error) (*Manager, *atomic.Int32) {
	t.Helper()
	r, body, sums := fixture()
	downloads := &atomic.Int32{}
	m := New(t.TempDir(), "2.4.1", "windows", "amd64", apply)
	m.client = &http.Client{Transport: transportFunc(func(req *http.Request) (*http.Response, error) {
		data := ""
		switch {
		case req.URL.String() == LatestURL:
			raw, _ := json.Marshal(r)
			data = string(raw)
		case strings.HasSuffix(req.URL.Path, "/SHA256SUMS"):
			downloads.Add(1)
			data = sums
		case strings.HasSuffix(req.URL.Path, ".exe"):
			downloads.Add(1)
			data = body
			if corrupt {
				data = "bad binary contents!"
			}
		default:
			return nil, fmt.Errorf("unexpected URL: %s", req.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(data)), Header: make(http.Header)}, nil
	})}
	return m, downloads
}
func until(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition timed out")
}

func TestChecksNeverDownloadAndConsentSelectsVersion(t *testing.T) {
	applied := make(chan string, 1)
	m, downloads := testManager(t, false, func(path, version, digest string) error {
		if err := Verify(path, digest); err != nil {
			return err
		}
		applied <- version
		return nil
	})
	if err := m.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !m.Status().Available || downloads.Load() != 0 {
		t.Fatal("check unexpectedly downloaded or missed update")
	}
	if err := m.Install("2.6.0"); err == nil {
		t.Fatal("accepted unselected version")
	}
	if err := m.Install("2.5.0"); err != nil {
		t.Fatal(err)
	}
	if err := m.Install("2.5.0"); err == nil {
		t.Fatal("accepted duplicate install")
	}
	select {
	case version := <-applied:
		if version != "2.5.0" {
			t.Fatal(version)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("not applied", m.Status())
	}
	if downloads.Load() != 2 {
		t.Fatal(downloads.Load())
	}
}
func TestBadChecksumNeverAppliesAndCanRetry(t *testing.T) {
	var applied atomic.Bool
	m, _ := testManager(t, true, func(string, string, string) error { applied.Store(true); return nil })
	if err := m.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Install("2.5.0"); err != nil {
		t.Fatal(err)
	}
	until(t, func() bool { return m.Status().Phase == "failed" })
	if applied.Load() {
		t.Fatal("corrupt executable applied")
	}
	if err := m.Install("2.5.0"); err != nil {
		t.Fatal("cannot retry", err)
	}
	until(t, func() bool { return m.Status().Phase == "failed" })
}
func TestPreferencesAndResultsSurviveRestart(t *testing.T) {
	m, downloads := testManager(t, false, nil)
	if err := m.SetAutoCheck(false); err != nil {
		t.Fatal(err)
	}
	if err := m.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.Install("2.5.0"); err == nil {
		t.Fatal("portable install accepted")
	}
	n := New(m.stateDir, "2.4.1", "windows", "amd64", nil)
	if n.Status().AutoCheck || !n.Status().Available || downloads.Load() != 0 {
		t.Fatal(n.Status())
	}
	n.client = &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
		t.Error("disabled automatic check made a request")
		return nil, fmt.Errorf("unexpected")
	})}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	n.Run(ctx)
	if err := WriteResult(m.stateDir, Result{Phase: "rolled_back", Target: "2.5.0", Error: "startup failed"}); err != nil {
		t.Fatal(err)
	}
	if n.Status().Phase != "rolled_back" {
		t.Fatal(n.Status())
	}
	if err := WriteJSON(ResultPath(m.stateDir), Result{Phase: "installing", Target: "2.5.0", PID: 0}); err != nil {
		t.Fatal(err)
	}
	if n.Status().Phase != "failed" {
		t.Fatal("interrupted helper not detected")
	}
	if err := n.SetAutoCheck(true); err != nil {
		t.Fatal(err)
	}
	if !New(m.stateDir, "2.4.1", "windows", "amd64", nil).Status().AutoCheck {
		t.Fatal("preference overwrite failed")
	}
}
func TestReleaseValidation(t *testing.T) {
	for _, tc := range []struct {
		candidate, current string
		newer              bool
	}{
		{"v2.10.0", "2.9.0", true}, {"v2.5.0", "2.5.0", false}, {"v2.4.0", "2.5.0", false}, {"v2.5.1-rc1", "2.5.0", false}, {"v2.5.0", "dev", false}, {"v02.5.0", "2.4.0", false},
	} {
		if Newer(tc.candidate, tc.current) != tc.newer {
			t.Errorf("%+v", tc)
		}
	}
	r, _, sums := fixture()
	name := r.Assets[0].Name
	if _, _, err := r.assets(name); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Release){
		func(r *Release) { r.Assets[0].URL = "https://example.org/file" },
		func(r *Release) { r.Assets = append(r.Assets, r.Assets[0]) },
		func(r *Release) { r.Assets[0].Size = maxBinarySize + 1 },
	} {
		bad, _, _ := fixture()
		mutate(&bad)
		if _, _, err := bad.assets(name); err == nil {
			t.Fatal("accepted invalid asset")
		}
	}
	if _, err := checksum(sums+sums, name); err == nil {
		t.Fatal("duplicate checksum accepted")
	}
	if _, err := checksum("bad "+name, name); err == nil {
		t.Fatal("bad checksum accepted")
	}
	for _, tag := range []string{"v2.5.0-rc1", "nightly"} {
		r.Tag = tag
		b, _ := json.Marshal(r)
		client := &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(b)))}, nil
		})}
		if _, err := fetchRelease(context.Background(), client, LatestURL, name); err == nil {
			t.Fatal("unstable release accepted")
		}
	}
}
func TestTransactionPreservesOrRestoresAllData(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			dir := t.TempDir()
			exe := filepath.Join(dir, "installed")
			candidate := filepath.Join(dir, "candidate")
			db := filepath.Join(dir, "usage.sqlite")
			wal := db + "-wal"
			newFile := db + "-shm"
			for p, s := range map[string]string{exe: "old program", candidate: "new program", db: "old database", wal: "committed WAL"} {
				if err := os.WriteFile(p, []byte(s), 0600); err != nil {
					t.Fatal(err)
				}
			}
			h := sha256.Sum256([]byte("new program"))
			starts, stops := 0, 0
			copyFile := func(a, b string) error {
				data, err := os.ReadFile(a)
				if err != nil {
					return err
				}
				return os.WriteFile(b, data, 0600)
			}
			tx := Transaction{Directory: dir, Candidate: candidate, Executable: exe, Digest: hex.EncodeToString(h[:]), DataFiles: []string{db, wal, newFile}, Copy: copyFile,
				Stop: func() error { stops++; return nil }, Start: func() error { starts++; return nil }, Healthy: func() error {
					if !fail {
						return nil
					}
					os.WriteFile(db, []byte("migrated database"), 0600)
					os.Remove(wal)
					os.WriteFile(newFile, []byte("new shared memory"), 0600)
					return fmt.Errorf("startup failure")
				}}
			phase, err := tx.Run()
			want := "updated"
			program := "new program"
			if fail {
				want = "rolled_back"
				program = "old program"
			}
			if phase != want || (err != nil) != fail {
				t.Fatalf("%s %v", phase, err)
			}
			for p, want := range map[string]string{exe: program, db: "old database", wal: "committed WAL"} {
				data, _ := os.ReadFile(p)
				if string(data) != want {
					t.Fatalf("%s: %s", p, data)
				}
			}
			if _, err := os.Stat(newFile); !os.IsNotExist(err) {
				t.Fatal("new-version sidecar survived rollback")
			}
			if fail && (starts != 2 || stops != 2) {
				t.Fatal(starts, stops)
			}
		})
	}
}
