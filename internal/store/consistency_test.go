package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/zJay26/codex-usage/internal/model"
)

func TestTimezonePersistsAndRepeatedHoursRemainDistinct(t *testing.T) {
	t.Setenv("CODEX_USAGE_TIMEZONE", "America/New_York")
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "usage.sqlite")
	a, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	t.Setenv("CODEX_USAGE_TIMEZONE", "Asia/Shanghai")
	b, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if b.Location().String() != "America/New_York" {
		t.Fatal("second process changed accounting timezone")
	}
	for _, value := range []string{"2026-11-01T05:30:00Z", "2026-11-01T06:30:00Z"} {
		at, _ := time.Parse(time.RFC3339, value)
		if _, err := b.InsertEvent(ctx, model.UsageEvent{ID: value, Timestamp: at, SessionID: "dst", Usage: model.TokenUsage{Input: 10, Total: 10}, Provenance: model.ProvenanceSessionJSONL}, ""); err != nil {
			t.Fatal(err)
		}
	}
	points, err := a.Timeseries(ctx, model.Filter{}, "hour")
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 2 || points[0].Time.Equal(points[1].Time) || points[0].Date != points[1].Date {
		t.Fatalf("fall-back hour identities: %+v", points)
	}
	if a.Revision() != b.Revision() {
		t.Fatal("data revision is process-local")
	}
	if err := a.ReadSnapshot(ctx, func(view *Store) error {
		revision := view.Revision()
		if _, err := b.InsertEvent(ctx, model.UsageEvent{ID: "later", SessionID: "dst", Usage: model.TokenUsage{Input: 20, Total: 20}, Provenance: model.ProvenanceSessionJSONL}, ""); err != nil {
			return err
		}
		summary, err := view.Summary(ctx, model.Filter{})
		if err != nil {
			return err
		}
		if summary.Usage.Total != 20 || view.Revision() != revision {
			t.Fatal("read snapshot changed mid-request")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestTimezoneBackfillRoundsUniformAndChangingFractionalOffsets(t *testing.T) {
	for _, tc := range []struct {
		zone     string
		at, want []string
	}{
		{"Asia/Kathmandu", []string{"2026-09-08T05:45:00Z"}, []string{"2026-09-08T05:15:00Z"}},
		{"Australia/Lord_Howe", []string{"2026-01-08T05:45:00Z", "2026-07-08T05:45:00Z"}, []string{"2026-01-08T05:00:00Z", "2026-07-08T05:30:00Z"}},
	} {
		t.Run(tc.zone, func(t *testing.T) {
			t.Setenv("CODEX_USAGE_TIMEZONE", tc.zone)
			st, err := Open(filepath.Join(t.TempDir(), "usage.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			ctx := context.Background()
			for _, value := range tc.at {
				at, _ := time.Parse(time.RFC3339, value)
				if _, err := st.InsertEvent(ctx, model.UsageEvent{ID: value, Timestamp: at, SessionID: "backfill", Usage: model.TokenUsage{Input: 10, Total: 10}, Provenance: model.ProvenanceSessionJSONL}, ""); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := st.db.Exec(`UPDATE usage_events SET hour_start=0`); err != nil {
				t.Fatal(err)
			}
			if err := st.initTimezone(ctx); err != nil {
				t.Fatal(err)
			}
			points, err := st.Timeseries(ctx, model.Filter{}, "hour")
			if err != nil {
				t.Fatal(err)
			}
			if len(points) != len(tc.want) {
				t.Fatalf("unexpected hours: %+v", points)
			}
			for i, point := range points {
				if point.Time.Format(time.RFC3339) != tc.want[i] || point.Usage.Total != 10 {
					t.Fatalf("incorrect migrated hour: %+v", point)
				}
			}
		})
	}
}
