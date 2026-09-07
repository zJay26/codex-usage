package store

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/zJay26/codex-usage/internal/model"
	"github.com/zJay26/codex-usage/internal/pricing"
)

func TestServiceModeAggregatesFiltersAndPricing(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, mode := range []string{"default", "priority", ""} {
		e := model.UsageEvent{ID: mode + "id", SessionID: "mixed", TurnID: mode, Model: "gpt-6-astra", Timestamp: time.Now(), Usage: model.TokenUsage{Input: 100, Output: 20, Total: 120}, Provenance: model.ProvenanceSessionJSONL, ServiceMode: model.ModeFromTier(mode, "jsonl_turn_context")}
		if _, err := st.InsertEvent(ctx, e, ""); err != nil {
			t.Fatal(err)
		}
	}
	for _, tt := range []struct {
		mode                          string
		total, regular, fast, unknown int64
	}{{"", 360, 240, 120, 120}, {"regular", 240, 240, 0, 120}, {"fast", 120, 0, 120, 0}, {"unknown", 120, 120, 0, 120}} {
		f := model.Filter{Mode: tt.mode}
		s, err := st.Summary(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		if s.Usage.Total != tt.total || s.Modes.Regular.Total != tt.regular || s.Modes.Fast.Total != tt.fast || s.Modes.Unknown.Total != tt.unknown {
			t.Fatalf("%s: %+v", tt.mode, s)
		}
		points, err := st.Timeseries(ctx, f, "hour")
		if err != nil || len(points) != 1 || points[0].Modes != s.Modes {
			t.Fatalf("points %+v %v", points, err)
		}
		items, err := st.Breakdown(ctx, f, "model", 10)
		if err != nil || len(items) != 1 || items[0].Modes != s.Modes {
			t.Fatalf("breakdown %+v %v", items, err)
		}
		sessions, err := st.Sessions(ctx, f, 10, 0)
		if err != nil || len(sessions) != 1 || sessions[0].Modes != s.Modes {
			t.Fatalf("sessions %+v %v", sessions, err)
		}
		raw, _ := pricing.NewBuilderForBasis(nil, pricing.FastWeightedBasis)
		agg, _ := pricing.NewBuilderForBasis(nil, pricing.FastWeightedBasis)
		sess, _ := pricing.NewBuilderForBasis(nil, pricing.FastWeightedBasis)
		if err := st.WalkPricingEvents(ctx, f, raw.Add); err != nil {
			t.Fatal(err)
		}
		if err := st.WalkPricingAggregates(ctx, f, agg.Add); err != nil {
			t.Fatal(err)
		}
		if err := st.WalkSessionPricingAggregates(ctx, f, []string{"mixed"}, sess.Add); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(raw.Report().Summary, agg.Report().Summary) || !reflect.DeepEqual(raw.Report().Summary, sess.Report().Summary) {
			t.Fatalf("pricing aggregation lost mode: raw=%+v agg=%+v sess=%+v", raw.Report().Summary, agg.Report().Summary, sess.Report().Summary)
		}
	}
}

func TestV7ModeMigrationIsAdditive(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "usage.sqlite")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.InsertEvent(ctx, model.UsageEvent{ID: "legacy", Usage: model.TokenUsage{Input: 12, Total: 12}, Provenance: model.ProvenanceSessionJSONL}, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`UPDATE meta SET value='7' WHERE key='schema_version'`, `ALTER TABLE usage_events DROP COLUMN service_mode`, `ALTER TABLE usage_events DROP COLUMN service_tier`, `ALTER TABLE usage_events DROP COLUMN mode_source`, `DROP TABLE turn_service_modes`, `DROP TABLE diagnostic_cursors`} {
		if _, err = st.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	st.Close()
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if reason, pending, err := st.HistoricalRebuildReason(ctx); err != nil || pending {
		t.Fatalf("unnecessary rebuild: %s %v", reason, err)
	}
	s, err := st.Summary(ctx, model.Filter{})
	if err != nil || s.Usage.Total != 12 || s.Modes.Unknown.Total != 12 || s.Modes.Regular.Total != 12 {
		t.Fatalf("migration lost history: %+v %v", s, err)
	}
}

func TestFastPricingKeepsEventBoundaries(t *testing.T) {
	ctx := context.Background()
	st, err := Open(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	for _, id := range []string{"a", "b", "c"} {
		_, err := st.InsertEvent(ctx, model.UsageEvent{ID: id, SessionID: "s", Timestamp: time.Now(), Model: "internal-luna", Usage: model.TokenUsage{Input: 1, Total: 1}, ServiceMode: model.ModeFromTier("fast", "test"), Provenance: model.ProvenanceSessionJSONL}, "")
		if err != nil {
			t.Fatal(err)
		}
	}
	overrides := map[string]pricing.Override{"internal-luna": {AliasOf: "gpt-5.6-luna"}}
	for _, walk := range []func(context.Context, model.Filter, func(model.UsageEvent) error) error{st.WalkPricingEvents, st.WalkPricingAggregates, func(ctx context.Context, f model.Filter, fn func(model.UsageEvent) error) error {
		return st.WalkSessionPricingAggregates(ctx, f, []string{"s"}, fn)
	}} {
		b, err := pricing.NewBuilderForBasis(overrides, pricing.FastWeightedBasis)
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		if err := walk(ctx, model.Filter{}, func(event model.UsageEvent) error {
			count++
			return b.Add(event)
		}); err != nil {
			t.Fatal(err)
		}
		if count != 3 || b.Report().Summary.USD != "0.000001500" {
			t.Fatalf("event boundaries changed by aggregation: count=%d %+v", count, b.Report().Summary)
		}
	}
}
