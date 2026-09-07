package usage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zJay26/codex-usage/internal/model"
	"github.com/zJay26/codex-usage/internal/store"
)

// Optional acceptance against an explicitly prepared disposable copy, never the
// installed ledger. Run after SQLite's online backup has captured the source.
func TestModeBackfillLocalSnapshot(t *testing.T) {
	path, home := os.Getenv("CODEX_USAGE_AUDIT_COPY"), os.Getenv("CODEX_USAGE_AUDIT_HOME")
	if path == "" || home == "" {
		t.Skip("set CODEX_USAGE_AUDIT_COPY and CODEX_USAGE_AUDIT_HOME for local acceptance")
	}
	abs, err := filepath.Abs(path)
	if err != nil || !strings.Contains(filepath.ToSlash(abs), "/dist/fast-acceptance/") {
		t.Fatal("audit requires disposable dist/fast-acceptance copy")
	}
	ctx := context.Background()
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	fingerprint := func() string {
		t.Helper()
		hash := sha256.New()
		enc := json.NewEncoder(hash)
		if err := st.WalkEvents(ctx, model.Filter{}, func(e model.UsageEvent) error { e.ServiceMode = model.ServiceMode{}; return enc.Encode(e) }); err != nil {
			t.Fatal(err)
		}
		return hex.EncodeToString(hash.Sum(nil))
	}
	beforeHash := fingerprint()
	before, err := st.Summary(ctx, model.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	s := Scanner{Store: st}
	s.scanDiagnosticModes(ctx, home)
	after, err := st.Summary(ctx, model.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	afterHash := fingerprint()
	if beforeHash != afterHash || before.Usage != after.Usage || before.EventCount != after.EventCount {
		t.Fatal("backfill altered original ledger")
	}
	samples := []map[string]any{}
	for _, pair := range [][2]string{{"01a07bb1-dfa0-7423-8eb2-3db0c8a7dffa", "01a07bb3-8bd7-7e12-88c6-c25d6534930d"}, {"01a07bb3-9940-7180-8aaf-940fa61e299d", "01a07bb3-c1e2-71d2-b4cc-4b03bd83d094"}, {"01a07bb4-68f1-7ed1-bd38-f9f6663ddbeb", "01a07bb4-7f79-7dc0-b341-9d01514644ad"}} {
		var total int64
		if err := st.WalkEvents(ctx, model.Filter{SessionID: pair[0]}, func(e model.UsageEvent) error {
			if e.TurnID == pair[1] {
				if e.ServiceMode.ServiceMode != model.ModeFast {
					t.Fatalf("sample not fast: %+v", e.ServiceMode)
				}
				total += e.Usage.Total
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if total == 0 {
			t.Fatal("missing expected sample")
		}
		samples = append(samples, map[string]any{"session_id": pair[0], "turn_id": pair[1], "fast_tokens": total})
	}
	progress, _ := st.DiagnosticProgress(ctx)
	for _, p := range progress {
		if p.State != "complete" {
			t.Fatalf("incomplete diagnostics: %+v", p)
		}
	}
	result := map[string]any{"before_ledger_sha256": beforeHash, "after_ledger_sha256": afterHash, "before": before, "after": after, "samples": samples, "mode_backfill": progress}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "audit.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	t.Logf("ledger=%s events=%d total=%d fast=%d unknown=%d", afterHash, after.EventCount, after.Usage.Total, after.Modes.Fast.Total, after.Modes.Unknown.Total)
}
