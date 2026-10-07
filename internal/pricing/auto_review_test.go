package pricing

import (
	"testing"

	"github.com/zJay26/codex-usage/internal/model"
)

func TestAutoReviewDefaultZeroPreservesUsage(t *testing.T) {
	for _, basis := range []string{Basis, FastWeightedBasis} {
		for _, tier := range []string{"default", "fast"} {
			for _, usage := range []model.TokenUsage{
				{Input: 1000, CachedInput: 200, CacheWriteInput: 100, Output: 100, ReasoningOutput: 50, Total: 1100},
				{Total: 1100},
			} {
				b, err := NewBuilderForBasis(nil, basis)
				if err != nil {
					t.Fatal(err)
				}
				e := model.UsageEvent{Model: " CODEX-AUTO-REVIEW ", Usage: usage, ServiceMode: model.ModeFromTier(tier, "test")}
				if err := b.Add(e); err != nil {
					t.Fatal(err)
				}
				r := b.Report()
				if r.Summary.USD != "0.000000000" || r.Summary.StandardBaseUSD != "0.000000000" || r.Summary.FastSurchargeUSD != "0.000000000" || r.Summary.PricedTokens != 1100 || r.Summary.UnpricedTokens != 0 || r.Summary.CoverageRatio != 1 || len(r.Summary.Reasons) != 0 {
					t.Fatalf("%s/%s: %+v", basis, tier, r.Summary)
				}
				if len(r.Models) != 1 || !r.Models[0].Usage.Equal(usage) {
					t.Fatalf("usage changed: %+v", r.Models)
				}
			}
		}
	}
}

func TestAutoReviewExplicitOverrideStillWins(t *testing.T) {
	for _, override := range []Override{
		{AliasOf: "gpt-6.1-sol"},
		{InputUSDPerMillion: "2", CachedInputUSDPerMillion: "0.1", CacheWriteInputUSDPerMillion: "2.5", OutputUSDPerMillion: "10"},
	} {
		b, err := NewBuilder(map[string]Override{"codex-auto-review": override})
		if err != nil {
			t.Fatal(err)
		}
		if err := b.Add(model.UsageEvent{Model: "codex-auto-review", Usage: model.TokenUsage{Input: 1000000, Total: 1000000}}); err != nil {
			t.Fatal(err)
		}
		if got := b.Report().Summary; got.USD != "2.000000000" || got.UnpricedTokens != 0 {
			t.Fatalf("override lost: %+v", got)
		}
	}
}

func TestAutoReviewDoesNotHideInvalidUsageOrMakeMainModelFree(t *testing.T) {
	bad, err := evaluateEvent(model.UsageEvent{Model: "codex-auto-review", Usage: model.TokenUsage{Input: 10, CachedInput: 11, Total: 10}}, nil)
	if err != nil || bad.unpricedTokens != 10 || len(bad.reasons) != 1 || bad.reasons[0].Kind != "invalid_token_categories" {
		t.Fatalf("invalid usage hidden: %+v %v", bad, err)
	}
	main, err := evaluateEvent(model.UsageEvent{Model: "gpt-6.1-sol", AgentType: "guardian", Usage: model.TokenUsage{Input: 1000000, Total: 1000000}}, nil)
	if err != nil || main.regularNano != 2000000000 {
		t.Fatalf("main model incorrectly free: %+v %v", main, err)
	}
}
