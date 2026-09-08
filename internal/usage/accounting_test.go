package usage

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/zJay26/codex-usage/internal/model"
	"github.com/zJay26/codex-usage/internal/store"
)

func accountingFixture(t *testing.T) (*store.Store, string, string) {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "codex")
	if err := os.MkdirAll(filepath.Join(home, "sessions"), 0700); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(root, "usage.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st, home, filepath.Join(home, "sessions", "a.jsonl")
}

func appendAccounting(t *testing.T, path, value string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(value); err != nil {
		t.Fatal(err)
	}
}

func accountingTotal(t *testing.T, st *store.Store, want int64) {
	t.Helper()
	summary, err := st.Summary(context.Background(), model.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Usage.Total != want {
		t.Fatalf("total=%d want=%d", summary.Usage.Total, want)
	}
}

const accountingMeta = `{"type":"session_meta","payload":{"id":"scope-test"}}` + "\n"

func accountingTurn(id string) string {
	return fmt.Sprintf(`{"type":"turn_context","payload":{"turn_id":%q,"model":"gpt-5.4","service_tier":"fast"}}`, id) + "\n"
}
func accountingToken(n, last int64) string {
	return tokenLine("2026-09-08T01:00:00Z", usage(n, 0, 0, 0, 0, n), usage(last, 0, 0, 0, 0, last)) + "\n"
}

func TestTurnCountersSmallerEqualLargerAndRestart(t *testing.T) {
	for _, next := range []int64{50, 100, 200} {
		t.Run(fmt.Sprint(next), func(t *testing.T) {
			st, home, path := accountingFixture(t)
			appendAccounting(t, path, accountingMeta+accountingTurn("one")+accountingToken(100, 100))
			if _, err := (&Scanner{Store: st}).Scan(context.Background(), []string{home}, false); err != nil {
				t.Fatal(err)
			}
			appendAccounting(t, path, accountingTurn("two")+accountingToken(next, next)+accountingToken(next, next))
			if _, err := (&Scanner{Store: st}).Scan(context.Background(), []string{home}, false); err != nil {
				t.Fatal(err)
			}
			accountingTotal(t, st, 100+next)
			// A new Scanner and a later snapshot of the same turn must only add its delta.
			appendAccounting(t, path, accountingToken(next+20, 20))
			if _, err := (&Scanner{Store: st}).Scan(context.Background(), []string{home}, false); err != nil {
				t.Fatal(err)
			}
			accountingTotal(t, st, 120+next)
			warnings, _ := st.Warnings(context.Background(), 100)
			for _, w := range warnings {
				if strings.Contains(w.Kind, "cumulative") {
					t.Fatal(w)
				}
			}
		})
	}
}

func TestIngestFailureRollsBackEventModesAndCursor(t *testing.T) {
	for _, failure := range []struct{ name, target string }{
		{"event", "INSERT ON usage_events"},
		{"progress", "UPDATE ON session_cursors"},
		{"cursor", "UPDATE ON file_cursors"},
	} {
		t.Run(failure.name, func(t *testing.T) {
			st, home, path := accountingFixture(t)
			appendAccounting(t, path, accountingMeta+accountingTurn("one")+accountingToken(100, 100))
			scan := &Scanner{Store: st}
			ctx := context.Background()
			if _, err := scan.Scan(ctx, []string{home}, false); err != nil {
				t.Fatal(err)
			}
			before, _, _ := st.GetCursor(ctx, path)
			db, err := sql.Open("sqlite", st.DBPath())
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if _, err = db.Exec(`CREATE TRIGGER fail_ingest BEFORE ` + failure.target + ` BEGIN SELECT RAISE(FAIL,'injected disk error'); END`); err != nil {
				t.Fatal(err)
			}
			appendAccounting(t, path, accountingTurn("two")+accountingToken(100, 100))
			if _, err = scan.Scan(ctx, []string{home}, false); err == nil {
				t.Fatal("storage error was swallowed")
			}
			accountingTotal(t, st, 100)
			after, _, _ := st.GetCursor(ctx, path)
			if after.Offset != before.Offset {
				t.Fatal("cursor advanced on rollback")
			}
			mode, err := st.ModeForTurn(ctx, home, "scope-test", "two")
			if err != nil || mode.ServiceMode != "unknown" {
				t.Fatalf("mode escaped rollback: %+v %v", mode, err)
			}
			if _, err = db.Exec(`DROP TRIGGER fail_ingest`); err != nil {
				t.Fatal(err)
			}
			if _, err = scan.Scan(ctx, []string{home}, false); err != nil {
				t.Fatal(err)
			}
			accountingTotal(t, st, 200)
		})
	}
}

func TestPartialRecordBeforeTypeIsRetried(t *testing.T) {
	st, home, path := accountingFixture(t)
	line := accountingToken(100, 100)
	appendAccounting(t, path, accountingMeta+line[:25])
	scan := &Scanner{Store: st}
	ctx := context.Background()
	if _, err := scan.Scan(ctx, []string{home}, false); err != nil {
		t.Fatal(err)
	}
	accountingTotal(t, st, 0)
	appendAccounting(t, path, line[25:])
	if _, err := scan.Scan(ctx, []string{home}, false); err != nil {
		t.Fatal(err)
	}
	accountingTotal(t, st, 100)
}

func TestTurnCounterReplayAndConcurrentWriters(t *testing.T) {
	st, home, path := accountingFixture(t)
	ctx := context.Background()
	content := accountingMeta + accountingTurn("one") + accountingToken(100, 100) + accountingTurn("two") + accountingToken(200, 200)
	appendAccounting(t, path, content)
	if _, err := (&Scanner{Store: st}).Scan(ctx, []string{home}, false); err != nil {
		t.Fatal(err)
	}
	// A copied physical rollout starts from its own replay position and adds only
	// the previously unseen tail, even after the turn counter scope is known.
	appendAccounting(t, filepath.Join(filepath.Dir(path), "b.jsonl"), content+accountingToken(250, 50))
	other, err := store.Open(st.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, db := range []*store.Store{st, other} {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := (&Scanner{Store: db}).Scan(ctx, []string{home}, false); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	accountingTotal(t, st, 350)
}

func TestSelectiveReaderKeepsPrefixAcrossFragments(t *testing.T) {
	line := strings.TrimSuffix(accountingToken(100, 100), "\n")
	record, _, complete, large, err := readSelectiveRecord(bufio.NewReaderSize(strings.NewReader(line), 16), 1<<20)
	if !complete || large || string(record) != line {
		t.Fatalf("split prefix lost: complete=%v large=%v err=%v record=%s", complete, large, err, record)
	}
}

func TestTurnClassificationAfterPartialPhysicalRestore(t *testing.T) {
	st, home, path := accountingFixture(t)
	ctx := context.Background()
	appendAccounting(t, path, accountingMeta+accountingTurn("one")+accountingToken(100, 100)+accountingTurn("two")+accountingToken(200, 200))
	scan := &Scanner{Store: st}
	if _, err := scan.Scan(ctx, []string{home}, false); err != nil {
		t.Fatal(err)
	}
	correction := tokenLine("2026-09-08T01:01:00Z", usage(200, 20, 0, 0, 0, 200), usage(0, 20, 0, 0, 0, 0)) + "\n"
	appendAccounting(t, filepath.Join(filepath.Dir(path), "b.jsonl"), accountingMeta+accountingTurn("two")+accountingToken(200, 200)+correction)
	if _, err := scan.Scan(ctx, []string{home}, false); err != nil {
		t.Fatal(err)
	}
	summary, err := st.Summary(ctx, model.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Usage.Total != 300 || summary.Usage.CachedInput != 20 {
		t.Fatalf("restored turn correction: %+v", summary.Usage)
	}
}

func TestPreUpgradeReplayPreservesLedgerUntilExplicitRebuild(t *testing.T) {
	st, home, path := accountingFixture(t)
	ctx := context.Background()
	if err := st.PutSessionProgress(ctx, "scope-test", 0, model.TokenUsage{Input: 100, Total: 100}, store.AccountingState{LegacyHistory: true}); err != nil {
		t.Fatal(err)
	}
	appendAccounting(t, path, accountingMeta+accountingTurn("one")+accountingToken(100, 100))
	_, err := (&Scanner{Store: st}).Scan(ctx, []string{home}, false)
	var rebuild *RebuildRequiredError
	if !errors.As(err, &rebuild) {
		t.Fatalf("unverified legacy replay did not stop: %v", err)
	}
	if _, ok, _ := st.GetCursor(ctx, path); ok {
		t.Fatal("ambiguous replay cursor committed")
	}
}

func TestTurnResumeWithoutCopiedSnapshotsUsesSharedTurnProgress(t *testing.T) {
	st, home, path := accountingFixture(t)
	ctx := context.Background()
	appendAccounting(t, path, accountingMeta+accountingTurn("one")+accountingToken(100, 100)+accountingTurn("two")+accountingToken(200, 200))
	scan := &Scanner{Store: st}
	if _, err := scan.Scan(ctx, []string{home}, false); err != nil {
		t.Fatal(err)
	}
	// A restored file may start at a later snapshot without copying the earlier
	// 200-token snapshot whose event ID is already in the ledger.
	appendAccounting(t, filepath.Join(filepath.Dir(path), "b.jsonl"), accountingMeta+accountingTurn("two")+accountingToken(250, 50))
	if _, err := scan.Scan(ctx, []string{home}, false); err != nil {
		t.Fatal(err)
	}
	accountingTotal(t, st, 350)
	// The original physical file may subsequently catch up and append again.
	appendAccounting(t, path, accountingToken(275, 25))
	if _, err := scan.Scan(ctx, []string{home}, false); err != nil {
		t.Fatal(err)
	}
	accountingTotal(t, st, 375)
}

func TestUnchangedRolloutBackfillsRelationshipsWithoutChangingUsage(t *testing.T) {
	st, home, path := accountingFixture(t)
	ctx := context.Background()
	meta := `{"type":"session_meta","payload":{"id":"scope-test","source":{"subagent":{"thread_spawn":{"parent_thread_id":"parent"}}},"forked_from_id":""}}` + "\n"
	appendAccounting(t, path, meta+accountingTurn("one")+accountingToken(100, 100))
	scan := &Scanner{Store: st}
	if _, err := scan.Scan(ctx, []string{home}, false); err != nil {
		t.Fatal(err)
	}
	// Simulate the relationship columns added to an already ingested database.
	db, err := sql.Open("sqlite", st.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE sessions SET parent_session_id=''; DELETE FROM relationship_backfills`); err != nil {
		t.Fatal(err)
	}
	if _, err := scan.Scan(ctx, []string{home}, false); err != nil {
		t.Fatal(err)
	}
	items, err := st.SessionRelationships(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if items["scope-test"].ParentSessionID != "parent" {
		t.Fatal("unchanged file did not backfill its explicit parent")
	}
	accountingTotal(t, st, 100)
}
