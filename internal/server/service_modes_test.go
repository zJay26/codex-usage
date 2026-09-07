package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zJay26/codex-usage/internal/model"
	"github.com/zJay26/codex-usage/internal/pricing"
	"github.com/zJay26/codex-usage/internal/store"
)

func TestModeAPIPriceBasisCacheAndExport(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, tier := range []string{"default", "priority", ""} {
		_, err = st.InsertEvent(context.Background(), model.UsageEvent{ID: "e" + tier, SessionID: "s", TurnID: "t" + tier, Timestamp: time.Now(), Model: "gpt-6-astra", Usage: model.TokenUsage{Input: 1000000, Total: 1000000}, ServiceMode: model.ModeFromTier(tier, "jsonl_turn_context"), Provenance: model.ProvenanceSessionJSONL}, "")
		if err != nil {
			t.Fatal(err)
		}
	}
	s := Server{Store: st}
	h := s.Handler()
	get := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		return w
	}
	for _, tt := range []struct{ query, want string }{{"", "30.000000000"}, {"?cost_basis=codex_fast_weighted", "45.000000000"}, {"?cost_basis=codex_fast_weighted&mode=fast", "25.000000000"}, {"?cost_basis=codex_fast_weighted&mode=regular", "20.000000000"}, {"?mode=unknown", "10.000000000"}} {
		var r pricing.Report
		json.Unmarshal(get("/api/v1/cost-estimate"+tt.query).Body.Bytes(), &r)
		if r.Summary.USD != tt.want {
			t.Fatalf("%s: %+v", tt.query, r)
		}
	}
	for _, path := range []string{"/api/v1/sessions", "/api/v1/session-estimates"} {
		for _, tt := range []struct{ query, want string }{{"?cost_basis=codex_fast_weighted&mode=fast", "25.000000000"}, {"?cost_basis=codex_fast_weighted&mode=regular", "20.000000000"}, {"", "30.000000000"}, {"?cost_basis=codex_fast_weighted&mode=fast", "25.000000000"}} {
			var r struct {
				Items []struct {
					Estimate pricing.Estimate `json:"estimate"`
				} `json:"items"`
			}
			if err = json.Unmarshal(get(path+tt.query).Body.Bytes(), &r); err != nil {
				t.Fatal(err)
			}
			if len(r.Items) != 1 || r.Items[0].Estimate.USD != tt.want {
				t.Fatalf("%s%s %+v", path, tt.query, r)
			}
		}
	}
	var events []model.UsageEvent
	json.Unmarshal(get("/api/v1/export?mode=fast").Body.Bytes(), &events)
	if len(events) != 1 || events[0].ServiceMode.ServiceMode != "fast" || events[0].ModeAssumed {
		t.Fatalf("export %+v", events)
	}
	csv := get("/api/v1/export?format=csv&mode=unknown").Body.String()
	if !strings.Contains(csv, "service_mode,service_tier,mode_source,mode_assumed") || !strings.Contains(csv, ",unknown,,jsonl_turn_context,true") {
		t.Fatal(csv)
	}
	for _, path := range []string{"/api/v1/summary?mode=bogus", "/api/v1/cost-estimate?cost_basis=bogus"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 400 {
			t.Fatal(path, w.Code)
		}
	}
}
