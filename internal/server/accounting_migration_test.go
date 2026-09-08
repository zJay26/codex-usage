package server

import (
	"context"
	"database/sql"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zJay26/codex-usage/internal/model"
	"github.com/zJay26/codex-usage/internal/store"
	"github.com/zJay26/codex-usage/internal/usage"
)

func TestCounterScopeUpgradeCanOnlyRecalculateThroughApprovedRescan(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	home := filepath.Join(root, "codex")
	if err := os.MkdirAll(filepath.Join(home, "sessions"), 0700); err != nil {
		t.Fatal(err)
	}
	content := `{"type":"session_meta","payload":{"id":"mixed-counter"}}` + "\n"
	for index, counters := range [][2]int{{100, 100}, {20, 20}, {50, 30}} {
		content += fmt.Sprintf(`{"type":"turn_context","payload":{"turn_id":"turn-%d","model":"gpt-5.4"}}`+"\n", index)
		content += fmt.Sprintf(`{"timestamp":"2026-09-08T01:00:0%dZ","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":%d,"total_tokens":%d},"last_token_usage":{"input_tokens":%d,"total_tokens":%d}}}}`+"\n",
			index, counters[0], counters[0], counters[1], counters[1])
	}
	if err := os.WriteFile(filepath.Join(home, "sessions", "mixed.jsonl"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "usage.sqlite")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.InsertEvent(ctx, model.UsageEvent{ID: "old-overcount", SessionID: "mixed-counter", Timestamp: time.Now(),
		Usage: model.TokenUsage{Input: 170, Total: 170}, Provenance: model.ProvenanceSessionJSONL, Confidence: model.ConfidenceExact,
	}, "mixed.jsonl"); err != nil {
		t.Fatal(err)
	}
	st.Close()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE meta SET value='9' WHERE key='schema_version'`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	st, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	srv := &Server{Store: st, Scanner: &usage.Scanner{Store: st}, Homes: func() ([]string, error) { return []string{home}, nil }}
	for _, step := range []struct {
		rebuild bool
		status  int
		total   int64
	}{
		{false, 409, 170}, // Preserve retained history until the user confirms.
		{true, 200, 150},  // Rebuild removes the repeated baseline.
		{false, 200, 150}, // Normal incremental operation resumes idempotently.
	} {
		request := httptest.NewRequest("POST", "http://127.0.0.1/api/v1/rescan", strings.NewReader(fmt.Sprintf(`{"rebuild":%v}`, step.rebuild)))
		response := httptest.NewRecorder()
		srv.Handler().ServeHTTP(response, request)
		if response.Code != step.status {
			t.Fatalf("rescan status=%d want=%d: %s", response.Code, step.status, response.Body.String())
		}
		if step.status == 409 && !strings.Contains(response.Body.String(), `"rebuild_required":true`) {
			t.Fatalf("Dashboard was not offered a rebuild: %s", response.Body.String())
		}
		summary, err := st.Summary(ctx, model.Filter{})
		if err != nil || summary.GrandTotal != step.total || summary.CoverageIncomplete != (step.status == 409) {
			t.Fatalf("history/quality after rescan: %+v, %v", summary, err)
		}
	}
}
