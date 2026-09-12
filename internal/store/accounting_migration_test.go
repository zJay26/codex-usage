package store

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zJay26/codex-usage/internal/model"
)

func TestCounterScopeMigrationPreservesHistoryAndRequiresExplicitRebuild(t *testing.T) {
	for _, version := range []int{8, 9, 10} {
		for _, history := range []bool{false, true} {
			t.Run(fmt.Sprintf("v%d/history=%v", version, history), func(t *testing.T) {
				ctx := context.Background()
				path := filepath.Join(t.TempDir(), "usage.sqlite")
				st, err := Open(path)
				if err != nil {
					t.Fatal(err)
				}
				if history {
					if _, err := st.InsertEvent(ctx, model.UsageEvent{
						ID: "existing", SessionID: "session", TurnID: "turn", Timestamp: time.Now(),
						Usage:      model.TokenUsage{Input: 170, Total: 170},
						Provenance: model.ProvenanceSessionJSONL, Confidence: model.ConfidenceExact,
					}, "original.jsonl"); err != nil {
						t.Fatal(err)
					}
					if err := st.PutCursor(ctx, FileCursor{Path: "original.jsonl", SessionID: "session", Offset: 123,
						Cumulative: model.TokenUsage{Input: 50, Total: 50},
						Accounting: AccountingState{Scope: "turn", LastTurnID: "turn"},
					}); err != nil {
						t.Fatal(err)
					}
				}
				if _, err := st.db.Exec(`UPDATE meta SET value=? WHERE key='schema_version'`, fmt.Sprint(version)); err != nil {
					t.Fatal(err)
				}
				if err := st.Close(); err != nil {
					t.Fatal(err)
				}
				wantPending := history
				// Reopening must preserve both the existing ledger and the marker.
				for reopen := 0; reopen < 2; reopen++ {
					st, err = Open(path)
					if err != nil {
						t.Fatal(err)
					}
					summary, err := st.Summary(ctx, model.Filter{})
					if err != nil {
						t.Fatal(err)
					}
					var wantTotal int64
					if history {
						wantTotal = 170
						cursor, ok, err := st.GetCursor(ctx, "original.jsonl")
						if err != nil || !ok || cursor.Offset != 123 || cursor.Cumulative.Total != 50 {
							t.Fatalf("migration changed the retained cursor: %+v, %v", cursor, err)
						}
					}
					if summary.GrandTotal != wantTotal || summary.CoverageIncomplete != wantPending {
						t.Fatalf("retained history or immediate quality notice: %+v", summary)
					}
					reason, pending, err := st.HistoricalRebuildReason(ctx)
					if err != nil || pending != wantPending || (pending && !strings.Contains(reason, "v2.6.4")) {
						t.Fatalf("pending=%v reason=%q err=%v", pending, reason, err)
					}
					if err := st.Close(); err != nil {
						t.Fatal(err)
					}
				}
				st, err = Open(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := st.ResetHistorical(ctx); err != nil {
					t.Fatal(err)
				}
				st.Close()
				st, err = Open(path)
				if err != nil {
					t.Fatal(err)
				}
				defer st.Close()
				if _, pending, err := st.HistoricalRebuildReason(ctx); err != nil || pending {
					t.Fatalf("explicit rebuild left a pending marker: %v, %v", pending, err)
				}
				if warnings, err := st.Warnings(ctx, 100); err != nil || len(warnings) != 0 {
					t.Fatalf("explicit rebuild left stale warnings: %v, %v", warnings, err)
				}
			})
		}
	}
}
