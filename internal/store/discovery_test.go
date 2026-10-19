package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/zJay26/codex-usage/internal/model"
)

func TestDiscoveryWarningLifecyclePreservesOtherWarningsAndRefreshesRevision(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	st, err := Open(filepath.Join(root, "usage.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	oldState := filepath.Join(home, "state_5.sqlite")
	newState := filepath.Join(home, "state_6.sqlite")
	if err := st.UpdateScanState(ctx, home, oldState, 1, "state_schema", "legacy"); err != nil {
		t.Fatal(err)
	}
	if err := st.AddWarning(ctx, "jsonl_record", oldState, "keep parser warning"); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(root, "other-home", "state_5.sqlite")
	if err := st.AddWarning(ctx, "state_schema", other, "keep other home"); err != nil {
		t.Fatal(err)
	}
	revision := st.Revision()
	for i := 0; i < 3; i++ {
		if err := st.UpdateScanState(ctx, home, newState, 1, "state_rollout_missing", "one missing"); err != nil {
			t.Fatal(err)
		}
		next := st.Revision()
		if i == 0 && next <= revision {
			t.Fatal("new warning failed to invalidate dashboard cache")
		}
		if i > 0 && next != revision {
			t.Fatal("unchanged discovery warning invalidated dashboard cache")
		}
		revision = next
	}
	warnings, err := st.Warnings(ctx, 10)
	if err != nil || len(warnings) != 3 {
		t.Fatalf("lost unrelated warnings: %+v %v", warnings, err)
	}
	for _, w := range warnings {
		if w.Path == oldState && w.Kind == "state_schema" {
			t.Fatal("old database warning retained")
		}
		if w.Kind == "state_rollout_missing" && w.Occurrences != 1 {
			t.Fatalf("repeated poll counted: %+v", w)
		}
	}
	if err := st.UpdateScanState(ctx, home, newState, 1, "state_rollout_missing", "two missing"); err != nil {
		t.Fatal(err)
	}
	warnings, err = st.Warnings(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range warnings {
		if w.Kind == "state_rollout_missing" && w.Occurrences != 2 {
			t.Fatalf("changed diagnostic not counted: %+v", w)
		}
	}
	if err := st.UpdateScanState(ctx, home, newState, 1, "", ""); err != nil {
		t.Fatal(err)
	}
	warnings, err = st.Warnings(ctx, 10)
	if err != nil || len(warnings) != 2 {
		t.Fatalf("resolution affected unrelated warnings: %+v %v", warnings, err)
	}
	if err := st.UpdateScanState(ctx, filepath.Dir(other), other, 0, "", ""); err != nil {
		t.Fatal(err)
	}
	// The parser warning must still mark coverage incomplete.
	summary, err := st.Summary(ctx, model.Filter{})
	if err != nil || !summary.CoverageIncomplete {
		t.Fatalf("accounting warning hidden: %+v %v", summary, err)
	}
	if _, err := st.db.ExecContext(ctx, `DELETE FROM warnings WHERE kind='jsonl_record'`); err != nil {
		t.Fatal(err)
	}
	revision = st.Revision()
	if err := st.UpdateScanState(ctx, home, newState, 0, "state_rollout_missing", "missing"); err != nil {
		t.Fatal(err)
	}
	if st.Revision() <= revision {
		t.Fatal("warning insertion did not advance revision")
	}
	revision = st.Revision()
	if err := st.UpdateScanState(ctx, home, newState, 0, "", ""); err != nil {
		t.Fatal(err)
	}
	if st.Revision() <= revision {
		t.Fatal("warning resolution did not advance revision")
	}
	summary, err = st.Summary(ctx, model.Filter{})
	if err != nil || summary.CoverageIncomplete {
		t.Fatalf("resolved discovery still marks incomplete coverage: %+v %v", summary, err)
	}
}
