package server

import (
	"net/http/httptest"
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
