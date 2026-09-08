package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/zJay26/codex-usage/internal/model"
	"github.com/zJay26/codex-usage/internal/pricing"
	"github.com/zJay26/codex-usage/internal/store"
)

func TestSessionCacheSeesAnotherWriterAndPricingChanges(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "usage.sqlite")
	a, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	overrides := map[string]pricing.Override{"custom": {AliasOf: "gpt-5.4"}}
	s := &Server{Store: a, LoadPricingOverrides: func() (map[string]pricing.Override, error) { return overrides, nil }}
	insert := func(id, modelName string, n int64) {
		t.Helper()
		_, err := b.InsertEvent(ctx, model.UsageEvent{ID: id, SessionID: "mixed", Model: modelName, Timestamp: time.Now(), Usage: model.TokenUsage{Input: n, Total: n}, Provenance: model.ProvenanceSessionJSONL, Confidence: model.ConfidenceExact}, "fixture")
		if err != nil {
			t.Fatal(err)
		}
	}
	get := func() sessionResponseItem {
		t.Helper()
		r := httptest.NewRequest("GET", "/api/v1/sessions?q=gpt-5.4", nil)
		w := httptest.NewRecorder()
		s.handleSessions(w, r)
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		var payload struct {
			Items []sessionResponseItem `json:"items"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if len(payload.Items) != 1 {
			t.Fatalf("rows=%+v", payload.Items)
		}
		return payload.Items[0]
	}
	insert("one", "gpt-5.4", 100)
	first := get()
	if first.Usage.Total != 100 {
		t.Fatal(first)
	}
	insert("two", "custom", 200)
	second := get()
	if second.Usage.Total != 300 || second.Estimate.PricedTokens != 300 {
		t.Fatalf("search/cache scope mismatch: %+v", second)
	}
	overrides["custom"] = pricing.Override{AliasOf: "gpt-5.5"}
	third := get()
	if third.Estimate.USD == second.Estimate.USD {
		t.Fatal("external pricing update reused old estimate")
	}
}

func TestTaskTreeTotalsFiltersCyclesAndMissingParents(t *testing.T) {
	rows := []store.SessionRow{}
	for id, total := range map[string]int64{"root": 100, "child": 30, "grandchild": 20, "orphan": 7, "cycle-a": 3, "cycle-b": 4} {
		rows = append(rows, store.SessionRow{SessionInfo: model.SessionInfo{SessionID: id}, Usage: model.TokenUsage{Input: total, Total: total}})
	}
	meta := map[string]model.SessionInfo{
		"root": {SessionID: "root"}, "child": {SessionID: "child", ParentSessionID: "root", ForkedFromID: "unrelated"}, "grandchild": {SessionID: "grandchild", ParentSessionID: "child"},
		"orphan": {SessionID: "orphan", ParentSessionID: "missing"}, "cycle-a": {SessionID: "cycle-a", ParentSessionID: "cycle-b"}, "cycle-b": {SessionID: "cycle-b", ParentSessionID: "cycle-a"},
	}
	nodes, _, roots := buildTaskTree(rows, meta)
	if nodes["root"].SubtreeUsage.Total != 150 || len(roots) != 3 {
		t.Fatalf("tree: %+v roots=%v", nodes["root"], roots)
	}
	var total int64
	for _, id := range roots {
		total += nodes[id].SubtreeUsage.Total
	}
	if total != 164 {
		t.Fatalf("duplicated tree accounting: %d", total)
	}
	if nodes["orphan"].RelationshipStatus != "missing_parent" {
		t.Fatal("orphan was hidden")
	}
	filtered := []store.SessionRow{{SessionInfo: model.SessionInfo{SessionID: "grandchild"}, Usage: model.TokenUsage{Total: 20}}}
	nodes, _, _ = buildTaskTree(filtered, meta)
	if !nodes["root"].ContextOnly || nodes["root"].Usage.Total != 0 || nodes["root"].SubtreeUsage.Total != 20 {
		t.Fatal("ancestor leaked unfiltered usage")
	}
}

func TestMeasurementDayUsesDSTAndFractionalOffsets(t *testing.T) {
	for _, tc := range []struct {
		zone, date, start string
		hours             int
	}{
		{"America/New_York", "2026-03-08", "2026-03-08T05:00:00Z", 23},
		{"America/New_York", "2026-11-01", "2026-11-01T04:00:00Z", 25},
		{"Asia/Kathmandu", "2026-09-08", "2026-09-07T18:15:00Z", 24},
	} {
		loc, err := time.LoadLocation(tc.zone)
		if err != nil {
			t.Fatal(err)
		}
		w := makeHourWindow(tc.date, loc, time.Now(), false)
		if w.Start.Format(time.RFC3339) != tc.start || w.Hours != tc.hours {
			t.Fatalf("%s: %+v", tc.zone, w)
		}
	}
}
