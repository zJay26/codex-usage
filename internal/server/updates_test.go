package server

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zJay26/codex-usage/internal/updater"
)

func TestUpdateAPIRequiresExplicitConfirmationAndSameOrigin(t *testing.T) {
	s := &Server{Updates: updater.New(t.TempDir(), "2.5.0", "windows", "amd64", nil)}
	for _, tc := range []struct {
		path, method, body, origin string
		code                       int
	}{
		{"", "GET", "", "", 200},
		{"/install", "GET", "", "", 405},
		{"/install", "POST", `{"version":"2.6.0"}`, "", 400},
		{"/install", "POST", `{"version":"2.6.0","confirm":true}`, "https://evil.example", 403},
		{"/install", "POST", `{"version":"2.6.0","confirm":true}`, "", 409},
		{"/preferences", "POST", `{"auto_check":false}`, "", 200},
		{"/preferences", "POST", `{"auto_check":false} {}`, "", 400},
	} {
		r := httptest.NewRequest(tc.method, "http://127.0.0.1:43189/api/v1/updates"+tc.path, strings.NewReader(tc.body))
		if tc.origin != "" {
			r.Header.Set("Origin", tc.origin)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != tc.code {
			t.Errorf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
	if s.Updates.Status().AutoCheck {
		t.Fatal("preference not applied")
	}
}

func TestUpdateDirectoryAPI(t *testing.T) {
	s := &Server{Updates: updater.New(t.TempDir(), "2.6.1", "windows", "amd64", nil)}
	want := filepath.Join(t.TempDir(), "downloads")
	data, _ := json.Marshal(map[string]string{"download_dir": want})
	r := httptest.NewRequest("POST", "http://127.0.0.1/api/v1/updates/preferences", strings.NewReader(string(data)))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 || s.Updates.Status().DownloadDir != want || !s.Updates.Status().AutoCheck {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	opened := false
	s.Updates.SetDirectoryOpener(func(path string) error { opened = path == want; return nil })
	r = httptest.NewRequest("POST", "http://127.0.0.1/api/v1/updates/open-directory", nil)
	r.Header.Set("Origin", "https://example.com")
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 403 || opened {
		t.Fatal("cross-origin folder open accepted")
	}
	r.Header.Del("Origin")
	w = httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 || !opened {
		t.Fatal(w.Code, opened)
	}
}
