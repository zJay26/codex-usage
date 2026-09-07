package usage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zJay26/codex-usage/internal/model"
	"github.com/zJay26/codex-usage/internal/store"
)

func diagnosticFixture(id, tier string) string {
	return fmt.Sprintf(`session_loop{thread_id=s}: Submission sub=Submission { id: %q, op: TurnInput { request: TurnInputRequest { input: UserInput { content: [Text { text: "service_tier: Some(Some(\"priority\")), Submission sub=Submission { id: \"fake\" }", text_elements: [] }] }, thread_settings: ThreadSettingsOverrides { service_tier: %s }, start: TurnStartOptions { service_tier: None } } }, trace: None }`, id, tier)
}

func TestDiagnosticModeParser(t *testing.T) {
	for _, tt := range []struct {
		tier, want string
		ok         bool
	}{
		{`Some(Some("priority"))`, model.ModeFast, true}, {`Some(Some("fast"))`, model.ModeFast, true},
		{`Some(Some("default"))`, model.ModeStandard, true}, {`Some(None)`, model.ModeStandard, true},
		{`None`, "", false}, {`Some(Some("ultrafast"))`, model.ModeUnknown, true},
	} {
		t.Run(tt.tier, func(t *testing.T) {
			turn, m, ok := parseDiagnosticMode(diagnosticFixture("turn", tt.tier))
			if ok != tt.ok || ok && (turn != "turn" || m.ServiceMode != tt.want) {
				t.Fatalf("turn=%s mode=%+v ok=%v", turn, m, ok)
			}
		})
	}
	for _, body := range []string{`Submission sub=Submission { id: "t", op: ThreadSettings { service_tier: Some(Some("fast")) } }`, `message="Submission sub=Submission { id: \"t\", op: TurnInput { service_tier: Some(Some(\"fast\")) } }"`, diagnosticFixture("t", `None`) + ` trailing="unterminated`} {
		if _, _, ok := parseDiagnosticMode(body); ok {
			t.Fatalf("accepted unsupported submission: %s", body)
		}
	}
	for _, tt := range []struct{ setting, want string }{{`None`, model.ModeFast}, {`Some(Some("priority"))`, model.ModeFast}, {`Some(None)`, model.ModeUnknown}} {
		body := strings.Replace(diagnosticFixture("turn", tt.setting), "start: TurnStartOptions { service_tier: None }", `start: TurnStartOptions { service_tier: Some(Some("priority")) }`, 1)
		turn, mode, ok := parseDiagnosticMode(body)
		if !ok || turn != "turn" || mode.ServiceMode != tt.want {
			t.Fatalf("start setting: turn=%s mode=%+v ok=%v", turn, mode, ok)
		}
	}
}

func TestUnchangedHistoricalJSONLBackfillsOnlyModeMetadata(t *testing.T) {
	ctx := context.Background()
	home, data := t.TempDir(), t.TempDir()
	dir := filepath.Join(home, "sessions")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "history.jsonl")
	lines := `{"type":"session_meta","payload":{"id":"history"}}
{"type":"turn_context","payload":{"turn_id":"historical-fast","model":"gpt-6-astra","service_tier":"priority"}}
` + tokenLine("2026-09-01T01:00:00Z", usage(80, 10, 0, 20, 2, 100), usage(80, 10, 0, 20, 2, 100)) + "\n"
	if err := os.WriteFile(path, []byte(lines), 0600); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(data, "usage.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := Scanner{Store: st}
	if _, err := s.Scan(ctx, []string{home}, false); err != nil {
		t.Fatal(err)
	}
	before, err := st.Events(ctx, store.EventQuery{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate an older scanner's complete token ledger and absent mode metadata.
	db, err := sql.Open("sqlite", filepath.Join(data, "usage.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`DELETE FROM mode_file_backfills`, `DELETE FROM turn_service_modes`, `UPDATE usage_events SET service_mode='unknown',service_tier='',mode_source='unavailable'`} {
		if _, err := db.Exec(q); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	db.Close()
	for range 2 {
		if _, err := s.Scan(ctx, []string{home}, false); err != nil {
			t.Fatal(err)
		}
	}
	after, err := st.Events(ctx, store.EventQuery{Limit: 100})
	if err != nil || !reflect.DeepEqual(before, after) || len(after) != 1 || after[0].ServiceMode.ServiceMode != model.ModeFast {
		t.Fatalf("metadata-only pass changed history: before=%+v after=%+v err=%v", before, after, err)
	}
	if !st.ModeFileChecked(ctx, path) {
		t.Fatal("completed mode backfill not checkpointed")
	}
}

func TestDiagnosticBackfillPreservesLedgerAndSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	path := filepath.Join(home, "logs_2.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE logs(id INTEGER PRIMARY KEY,ts INTEGER,thread_id TEXT,target TEXT,feedback_log_body TEXT)`)
	if err != nil {
		t.Fatal(err)
	}
	stPath := filepath.Join(t.TempDir(), "usage.sqlite")
	st, err := store.Open(stPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { st.Close() }()
	e := model.UsageEvent{ID: "event", SessionID: "s", TurnID: "t", CodexHome: home, Model: "gpt-6-astra", Timestamp: time.Now(), Usage: model.TokenUsage{Input: 80, Output: 20, Total: 100}, Provenance: model.ProvenanceSessionJSONL}
	if _, err := st.InsertEvent(ctx, e, ""); err != nil {
		t.Fatal(err)
	}
	before, _ := st.Summary(ctx, model.Filter{})
	insert := func(id int, thread, body string) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO logs VALUES(?,100,?,'codex_core::session::handlers',?)`, id, thread, body); err != nil {
			t.Fatal(err)
		}
	}
	insert(1, "other", diagnosticFixture("t", `Some(Some("fast"))`))
	s := Scanner{Store: st}
	s.scanDiagnosticModes(ctx, home)
	x, _ := st.Summary(ctx, model.Filter{})
	if x.Modes.Fast.Total != 0 {
		t.Fatal("cross-thread contamination")
	}
	insert(2, "s", diagnosticFixture("t", `Some(Some("priority"))`))
	s.scanDiagnosticModes(ctx, home)
	after, _ := st.Summary(ctx, model.Filter{})
	if after.Usage != before.Usage || after.Modes.Fast.Total != 100 || after.Modes.Unknown.Total != 0 {
		t.Fatalf("bad backfill %+v", after)
	}
	st.Close()
	st, err = store.Open(stPath)
	if err != nil {
		t.Fatal(err)
	}
	s.Store = st
	s.scanDiagnosticModes(ctx, home)
	if _, err = db.Exec(`DELETE FROM logs`); err != nil {
		t.Fatal(err)
	}
	s.scanDiagnosticModes(ctx, home)
	again, _ := st.Summary(ctx, model.Filter{})
	if again.Usage != before.Usage || again.Modes.Fast.Total != 100 {
		t.Fatal("restart/retention changed ledger")
	}
	// A rotated database can reuse ids. A contradictory direct record is unknown.
	insert(1, "s", diagnosticFixture("t", `Some(None)`))
	s.scanDiagnosticModes(ctx, home)
	conflict, _ := st.Summary(ctx, model.Filter{})
	if conflict.Modes.Unknown.Total != 100 {
		t.Fatal("conflict did not fail closed")
	}
	if err := st.PutTurnModes(ctx, []store.TurnMode{{Home: home, SessionID: "s", TurnID: "t", Mode: model.ModeFromTier("priority", "jsonl_turn_context")}}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutTurnModes(ctx, []store.TurnMode{{Home: home, SessionID: "s", TurnID: "t", Mode: model.ModeFromTier("default", "diagnostic_turn_input")}}); err != nil {
		t.Fatal(err)
	}
	final, _ := st.Summary(ctx, model.Filter{})
	if final.Modes.Fast.Total != 100 || final.Usage != before.Usage {
		t.Fatal("JSONL priority failed")
	}
}

func TestScannerJSONLModeSwitchAndMissingDoNotInherit(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "sessions")
	os.MkdirAll(dir, 0700)
	lines := `{"type":"session_meta","payload":{"id":"s"}}
{"type":"turn_context","payload":{"turn_id":"t1","model":"gpt-6-astra","service_tier":"priority"}}
` + tokenLine("2026-09-07T01:00:00Z", usage(80, 10, 0, 20, 2, 100), usage(80, 10, 0, 20, 2, 100)) + `
{"type":"turn_context","payload":{"turn_id":"t2","model":"gpt-6-astra","service_tier":"default"}}
` + tokenLine("2026-09-07T02:00:00Z", usage(40, 5, 0, 10, 1, 50), usage(40, 5, 0, 10, 1, 50)) + `
{"type":"turn_context","payload":{"turn_id":"t3","model":"gpt-6-astra"}}
` + tokenLine("2026-09-07T03:00:00Z", usage(16, 0, 0, 4, 0, 20), usage(16, 0, 0, 4, 0, 20)) + "\n"
	os.WriteFile(filepath.Join(dir, "rollout.jsonl"), []byte(lines), 0600)
	st, err := store.Open(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := Scanner{Store: st}
	for range 2 {
		if _, err = s.Scan(context.Background(), []string{home}, false); err != nil {
			t.Fatal(err)
		}
	}
	x, err := st.Summary(context.Background(), model.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if x.Usage.Total != 170 || x.Modes.Fast.Total != 100 || x.Modes.Regular.Total != 70 || x.Modes.Unknown.Total != 20 {
		t.Fatalf("%+v", x)
	}
}
