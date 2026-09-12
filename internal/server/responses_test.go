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

func TestCompactionUpgradePreservesV10UntilExplicitRebuild(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "codex")
	path := filepath.Join(root, "usage.sqlite")
	ctx := context.Background()
	if err := os.MkdirAll(filepath.Join(home, "sessions"), 0700); err != nil {
		t.Fatal(err)
	}
	content := `{"type":"session_meta","payload":{"id":"session"}}` + "\n" + `{"type":"turn_context","payload":{"turn_id":"turn","model":"gpt-6-astra"}}` + "\n"
	for i, n := range []int{100, 30} {
		total := 100
		if i == 1 {
			total = 130
		}
		content += fmt.Sprintf(`{"timestamp":"2026-09-12T01:00:0%dZ","type":"token_usage_record","payload":{"thread_id":"session","turn_id":"turn","response_id":"response-%d","usage":{"input_tokens":%d,"total_tokens":%d},"turn_token_usage":{"input_tokens":%d,"total_tokens":%d}}}`+"\n", i, i, n, n, total, total)
	}
	if err := os.WriteFile(filepath.Join(home, "sessions", "a.jsonl"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.InsertEvent(ctx, model.UsageEvent{ID: "old", SessionID: "session", TurnID: "turn", Timestamp: time.Now(), Usage: model.TokenUsage{Input: 100, Total: 100}, Provenance: model.ProvenanceSessionJSONL}, "a.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE meta SET value='10' WHERE key='schema_version'; DROP TABLE response_records`); err != nil {
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
		approved bool
		status   int
		total    int64
	}{{false, 409, 100}, {true, 200, 130}, {false, 200, 130}} {
		w := httptest.NewRecorder()
		request := httptest.NewRequest("POST", "http://127.0.0.1/api/v1/rescan", strings.NewReader(fmt.Sprintf(`{"rebuild":%v}`, step.approved)))
		srv.Handler().ServeHTTP(w, request)
		if w.Code != step.status {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		s, err := st.Summary(ctx, model.Filter{})
		if err != nil || s.Usage.Total != step.total {
			t.Fatalf("%+v %v", s, err)
		}
	}
}
