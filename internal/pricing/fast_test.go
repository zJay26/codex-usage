package pricing

import (
	"math"
	"testing"

	"github.com/zJay26/codex-usage/internal/model"
)

func TestFastMultiplierIntegerRounding(t *testing.T) {
	for _, tt := range []struct{ base, want int64 }{{0, 0}, {1, 3}, {2, 5}, {3, 8}, {1000001, 2500003}} {
		got, err := tokenCost(tt.base, 1, 5, 2)
		if err != nil || got != tt.want {
			t.Fatalf("base=%d got=%d want=%d err=%v", tt.base, got, tt.want, err)
		}
	}
}

func TestFastWeightedPricing(t *testing.T) {
	for _, tt := range []struct{ name, want string }{
		{"gpt-6-astra", "150.000000000"}, {"gpt-5.6-sol", "87.500000000"},
		{"gpt-5.6-terra", "35.000000000"}, {"gpt-5.6-luna", "3.500000000"},
		{"gpt-5.5", "87.500000000"}, {"gpt-5.6", "87.500000000"},
		{"gpt-5.5-2026-09-01", "87.500000000"}, {"gpt-5.4", "35.000000000"}, {"gpt-5.4-mini", "0.000000000"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			b, _ := NewBuilderForBasis(nil, FastWeightedBasis)
			e := model.UsageEvent{Model: tt.name, ServiceMode: model.ModeFromTier("priority", "jsonl_turn_context"), Usage: model.TokenUsage{Input: 1000000, Output: 1000000, Total: 2000000}}
			if err := b.Add(e); err != nil {
				t.Fatal(err)
			}
			r := b.Report()
			if r.Summary.USD != tt.want {
				t.Fatalf("%+v", r.Summary)
			}
			if tt.name == "gpt-5.4-mini" && r.Summary.UnpricedTokens != 2000000 {
				t.Fatal("missing multiplier treated as free")
			}
		})
	}
	b, _ := NewBuilderForBasis(nil, FastWeightedBasis)
	base, _ := NewBuilder(nil)
	e := model.UsageEvent{Model: "gpt-6-astra", Usage: model.TokenUsage{Input: 1000000, CachedInput: 100000, CacheWriteInput: 100000, Output: 100000, Total: 1100000}}
	base.Add(e)
	b.Add(e)
	e.ServiceMode = model.ModeFromTier("priority", "diagnostic_turn_input")
	b.Add(e)
	r := b.Report()
	if r.Modes.Regular.Total != 1100000 || r.Modes.Fast.Total != 1100000 || r.Modes.Unknown.Total != 1100000 {
		t.Fatal(r.Modes)
	}
	if r.Summary.RegularModeUSD != base.Report().Summary.USD || r.Summary.FastModeUSD != "35.875000000" || r.Summary.StandardBaseUSD != "28.700000000" || r.Summary.FastSurchargeUSD != "21.525000000" {
		t.Fatalf("%+v", r.Summary)
	}
}

func TestFastAliasOverridesAndOverflow(t *testing.T) {
	b, err := NewBuilderForBasis(map[string]Override{"custom": {AliasOf: "gpt-6-astra"}}, FastWeightedBasis)
	if err != nil {
		t.Fatal(err)
	}
	e := model.UsageEvent{Model: "custom", ServiceMode: model.ModeFromTier("fast", "test"), Usage: model.TokenUsage{Input: 1000000, Total: 1000000}}
	if err := b.Add(e); err != nil {
		t.Fatal(err)
	}
	if b.Report().Summary.USD != "25.000000000" {
		t.Fatal(b.Report())
	}
	e.Usage = model.TokenUsage{Input: math.MaxInt64 / 20000, Total: math.MaxInt64 / 20000}
	if err := b.Add(e); err == nil {
		t.Fatal("expected weighted overflow")
	}
	if _, err := NewBuilderForBasis(nil, "other"); err == nil {
		t.Fatal("unknown basis accepted")
	}
	custom, err := NewBuilderForBasis(map[string]Override{"custom": {InputUSDPerMillion: "1", CachedInputUSDPerMillion: "0.1", CacheWriteInputUSDPerMillion: "1.25", OutputUSDPerMillion: "6"}}, FastWeightedBasis)
	if err != nil {
		t.Fatal(err)
	}
	e.Usage = model.TokenUsage{Input: 1000000, Total: 1000000}
	if err := custom.Add(e); err != nil {
		t.Fatal(err)
	}
	r := custom.Report().Summary
	if r.USD != "0.000000000" || r.StandardBaseUSD != "1.000000000" || r.UnpricedTokens != 1000000 || len(r.Reasons) != 1 || r.Reasons[0].Kind != "fast_multiplier_missing" {
		t.Fatalf("custom price invented a Fast multiplier: %+v", r)
	}
}
