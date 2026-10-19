package usage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zJay26/codex-usage/internal/model"
	"github.com/zJay26/codex-usage/internal/store"
)

func writeDiscoveryRollout(t *testing.T, path, id string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	content := fmt.Sprintf(`{"type":"session_meta","payload":{"id":%q}}`, id) + "\n" +
		tokenLine("2026-10-19T01:00:00Z", usage(8, 1, 0, 2, 0, 10), usage(8, 1, 0, 2, 0, 10)) + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeDiscoveryState(t *testing.T, home, id, path string) string {
	t.Helper()
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(home, "state_5.sqlite")
	db, err := sql.Open("sqlite", state)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`CREATE TABLE threads(id TEXT, rollout_path TEXT, title TEXT); INSERT INTO threads VALUES(?,?,?)`, id, path, "state title"); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestDiscoveryRecoversMovedRolloutsWithoutDoubleCounting(t *testing.T) {
	for _, scenario := range []string{"moved home", "archived", "Windows path", "relative path"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "new-home")
			original := filepath.Join(root, "old-home", "sessions", "rollout-one.jsonl")
			actual := filepath.Join(home, "sessions", "2026", "10", "19", "rollout-one.jsonl")
			switch scenario {
			case "archived":
				original = filepath.Join(home, "sessions", "rollout-one.jsonl")
				actual = filepath.Join(home, "archived_sessions", "rollout-one.jsonl")
			case "Windows path":
				original = `\\?\C:\Users\old\.codex\sessions\rollout-one.jsonl`
			case "relative path":
				original = filepath.Join("sessions", "2026", "10", "19", "rollout-one.jsonl")
			}
			writeDiscoveryRollout(t, actual, "one")
			state := writeDiscoveryState(t, home, "one", original)
			before, err := os.ReadFile(state)
			if err != nil {
				t.Fatal(err)
			}
			discovery := DiscoverHome(context.Background(), home)
			if discovery.Warning != "" || len(discovery.Paths) != 1 || discovery.Paths[0] != actual || discovery.Sessions[0].RolloutPath != actual {
				t.Fatalf("moved rollout not reconciled: %+v", discovery)
			}
			st, err := store.Open(filepath.Join(root, "usage.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			scanner := &Scanner{Store: st}
			for i := 0; i < 2; i++ {
				result, err := scanner.Scan(context.Background(), []string{home}, false)
				if err != nil || result.Warnings != 0 {
					t.Fatalf("scan %d: %+v %v", i, result, err)
				}
			}
			summary, err := st.Summary(context.Background(), model.Filter{})
			if err != nil || summary.GrandTotal != 10 || summary.CoverageIncomplete {
				t.Fatalf("bad recovered accounting: %+v %v", summary, err)
			}
			session, err := st.Session(context.Background(), "one")
			if err != nil || session.Title != "state title" || session.RolloutPath != actual {
				t.Fatalf("metadata lost: %+v %v", session, err)
			}
			after, err := os.ReadFile(state)
			if err != nil || string(before) != string(after) {
				t.Fatal("source state database changed")
			}
		})
	}
}

func TestDiscoveryDoesNotResolveUnverifiedOrAmbiguousFiles(t *testing.T) {
	for _, scenario := range []string{"wrong owner", "ambiguous", "copied parent", "partial header", "malformed header"} {
		t.Run(scenario, func(t *testing.T) {
			home := filepath.Join(t.TempDir(), "home")
			actual := filepath.Join(home, "sessions", "same.jsonl")
			writeDiscoveryRollout(t, actual, "other")
			switch scenario {
			case "ambiguous":
				writeDiscoveryRollout(t, actual, "wanted")
				writeDiscoveryRollout(t, filepath.Join(home, "archived_sessions", "same.jsonl"), "wanted")
			case "copied parent":
				f, err := os.OpenFile(actual, os.O_APPEND|os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				_, err = f.WriteString(`{"type":"session_meta","payload":{"id":"wanted"}}` + "\n")
				f.Close()
				if err != nil {
					t.Fatal(err)
				}
			case "malformed header":
				if err := os.WriteFile(actual, []byte(`{"type":"session_meta",BROKEN}`+"\n"+`{"type":"session_meta","payload":{"id":"wanted"}}`+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "partial header":
				if err := os.WriteFile(actual, []byte(`{"type":"session_meta","payload":{"id":"wanted"}}`), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			original := filepath.Join(home, "missing", "same.jsonl")
			writeDiscoveryState(t, home, "wanted", original)
			d := DiscoverHome(context.Background(), home)
			if d.WarningKind != "state_rollout_missing" || d.Sessions[0].RolloutPath != original || len(d.Paths) == 0 {
				t.Fatalf("unsafe path match: %+v", d)
			}
		})
	}
}

func TestDiscoveryWarningsReconcileAfterSourceRestored(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	actual := filepath.Join(home, "sessions", "restored.jsonl")
	state := writeDiscoveryState(t, home, "one", actual)
	st, err := store.Open(filepath.Join(root, "usage.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.AddWarning(ctx, "state_schema", state, "legacy missing path warning"); err != nil {
		t.Fatal(err)
	}
	scanner := &Scanner{Store: st}
	for i := 0; i < 3; i++ {
		if _, err := scanner.Scan(ctx, []string{home}, false); err != nil {
			t.Fatal(err)
		}
	}
	warnings, err := st.Warnings(ctx, 10)
	if err != nil || len(warnings) != 1 || warnings[0].Kind != "state_rollout_missing" || warnings[0].Occurrences != 1 || !strings.Contains(warnings[0].Detail, "重建无法恢复") {
		t.Fatalf("repeated or misclassified warning: %+v %v", warnings, err)
	}
	writeDiscoveryRollout(t, actual, "one")
	if _, err := scanner.Scan(ctx, []string{home}, false); err != nil {
		t.Fatal(err)
	}
	warnings, err = st.Warnings(ctx, 10)
	if err != nil || len(warnings) != 0 {
		t.Fatalf("resolved warning remains: %+v %v", warnings, err)
	}
	status, err := st.Status(ctx)
	if err != nil || status.WarningCount != 0 || status.CodexHomes[0].Warning != "" {
		t.Fatalf("stale status: %+v %v", status, err)
	}
}
