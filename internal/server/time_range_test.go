package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/zJay26/codex-usage/internal/model"
	"github.com/zJay26/codex-usage/internal/pricing"
	"github.com/zJay26/codex-usage/internal/store"
)

func TestMinuteRangeAPIUsesEventBoundariesAndAccountingTimezone(t *testing.T) {
	t.Setenv("CODEX_USAGE_TIMEZONE", "Asia/Shanghai")
	st, err := store.Open(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	start, _ := time.Parse(time.RFC3339, "2026-09-16T23:59:00+08:00")
	end := start.Add(3 * time.Minute)
	// One session spans both bounds and midnight. Whole-session or daily
	// aggregation would include the outside events and produce the wrong total.
	for i, at := range []time.Time{start.Add(-time.Second), start, end.Add(-time.Second), end} {
		n := int64(i + 1)
		_, err := st.InsertEvent(context.Background(), model.UsageEvent{
			ID: fmt.Sprintf("range-%d", i), Timestamp: at, ObservedAt: at,
			SessionID: "spanning-session", Model: "gpt-5.4",
			Usage:      model.TokenUsage{Input: 80 * n, CachedInput: 20 * n, Output: 20 * n, ReasoningOutput: 5 * n, Total: 100 * n},
			Provenance: model.ProvenanceSessionJSONL, Confidence: model.ConfidenceExact,
		}, "range-fixture.jsonl")
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.InsertEvent(context.Background(), model.UsageEvent{
		ID: "undated", ObservedAt: start, Model: "gpt-5.4",
		Usage:      model.TokenUsage{Input: 888, Total: 888},
		Provenance: model.ProvenanceSessionJSONL, Confidence: model.ConfidenceGapFallback,
	}, "undated-fixture.jsonl"); err != nil {
		t.Fatal(err)
	}
	handler := (&Server{Store: st}).Handler()
	for _, tc := range []struct {
		name, since, until string
		want, events       int64
	}{
		{"cross midnight", "2026-09-16T23:59", "2026-09-17T00:02", 500, 2},
		{"equivalent UTC", "2026-09-16T15:59:00Z", "2026-09-16T16:02:00Z", 500, 2},
		{"empty minute", "2026-09-17T00:00", "2026-09-17T00:01", 0, 0},
		{"adjacent range", "2026-09-17T00:02", "2026-09-17T00:03", 400, 1},
		{"unrestricted dates", "1900-01-01T00:00", "2200-01-01T00:00", 1000, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query := url.Values{"since": {tc.since}, "until": {tc.until}, "fill_days": {"0"}}
			get := func(endpoint string, target any) {
				t.Helper()
				response := httptest.NewRecorder()
				handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, endpoint+"?"+query.Encode(), nil))
				if response.Code != http.StatusOK {
					t.Fatalf("%s: status=%d body=%s", endpoint, response.Code, response.Body)
				}
				if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
					t.Fatal(err)
				}
			}
			var summary model.Summary
			get("/api/v1/summary", &summary)
			if summary.GrandTotal != tc.want || summary.Usage.Total != tc.want || summary.EventCount != tc.events ||
				summary.Usage.Input != tc.want*80/100 || summary.Usage.CachedInput != tc.want*20/100 ||
				summary.Usage.Output != tc.want*20/100 || summary.Usage.ReasoningOutput != tc.want*5/100 {
				t.Fatalf("unexpected range summary: %+v", summary)
			}
			var cost pricing.Report
			get("/api/v1/cost-estimate", &cost)
			if cost.Summary.PricedTokens != tc.want || cost.Summary.UnpricedTokens != 0 {
				t.Fatalf("cost coverage disagrees with token range: %+v", cost.Summary)
			}
			var series struct {
				Points []model.Point `json:"points"`
			}
			get("/api/v1/timeseries", &series)
			var total int64
			for _, point := range series.Points {
				total += point.Usage.Total
			}
			if total != tc.want {
				t.Fatalf("daily points=%d want %d", total, tc.want)
			}
		})
	}
	for _, query := range []url.Values{
		{"since": {"2026-09-16T23:59"}, "until": {"2026-09-16T23:59"}},
		{"since": {"2026-09-17T00:01"}, "until": {"2026-09-16T23:59"}},
		{"since": {"2026-02-30T12:00"}, "until": {"2026-03-01T00:00"}},
		{"since": {"2026-09-16T23:60"}, "until": {"2026-09-17T00:01"}},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/summary?"+query.Encode(), nil))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid range %v: status=%d body=%s", query, response.Code, response.Body)
		}
	}
}

func TestLocalMinuteDSTBoundaries(t *testing.T) {
	for _, tc := range []struct {
		zone, value, want string
	}{
		{"America/New_York", "2026-03-08T02:30", ""},
		{"America/New_York", "2026-11-01T01:30", "2026-11-01T05:30:00Z"},
		{"America/New_York", "2026-11-01T01:30:00-05:00", "2026-11-01T06:30:00Z"},
		{"Australia/Lord_Howe", "2026-04-05T01:45", "2026-04-04T14:45:00Z"},
		{"Australia/Lord_Howe", "2026-10-04T02:15", ""},
	} {
		t.Run(tc.zone+"/"+tc.value, func(t *testing.T) {
			loc, err := time.LoadLocation(tc.zone)
			if err != nil {
				t.Fatal(err)
			}
			got, err := parseAbsoluteTimeIn(tc.value, loc)
			if tc.want == "" {
				if err == nil {
					t.Fatalf("nonexistent minute accepted as %v", got)
				}
				return
			}
			if err != nil || got.UTC().Format(time.RFC3339) != tc.want {
				t.Fatalf("got %v, %v; want %s", got, err, tc.want)
			}
		})
	}
}
