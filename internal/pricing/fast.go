package pricing

import (
	"fmt"

	"github.com/zJay26/codex-usage/internal/model"
)

const FastWeightedBasis = "codex_fast_weighted"
const FastRulesAsOf = "2026-09-07"
const FastRulesSource = "https://learn.chatgpt.com/docs/agent-configuration/speed"

func ValidBasis(basis string) bool {
	return basis == "" || basis == Basis || basis == FastWeightedBasis
}

func NewBuilderForBasis(overrides map[string]Override, basis string) (*Builder, error) {
	if !ValidBasis(basis) {
		return nil, fmt.Errorf("不支持的计价口径 %q", basis)
	}
	b, err := NewBuilder(overrides)
	if err != nil {
		return nil, err
	}
	if basis != "" {
		b.basis = basis
	}
	return b, nil
}

// Deliberate allowlist; mini, Spark, future models and custom rates are not
// assigned a subscription multiplier merely by sharing a name prefix.
func FastMultiplier(canonical string) (int64, int64, bool) {
	// Only catalog aliases and dated snapshots can resolve to this allowlist.
	if rate, found, err := Resolve(canonical, nil); err == nil && found {
		canonical = rate.CanonicalModel
	}
	switch canonical {
	case "gpt-6-astra", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna", "gpt-5.5":
		return 5, 2, true
	case "gpt-5.4":
		return 2, 1, true
	default:
		return 0, 0, false
	}
}

func evaluateWithBasis(event model.UsageEvent, overrides map[string]Override, basis string) (evaluatedEvent, error) {
	out, err := evaluateEvent(event, overrides)
	if err != nil {
		return out, err
	}
	out.mode = event.ServiceMode.Normalized().ServiceMode
	for _, n := range []int64{out.regularNano, out.cachedNano, out.cacheWriteNano, out.outputNano} {
		out.baseNano, err = checkedAdd(out.baseNano, n)
		if err != nil {
			return out, err
		}
	}
	if basis != FastWeightedBasis || out.mode != model.ModeFast || out.pricedTokens == 0 {
		return out, nil
	}
	rate, found, err := Resolve(event.Model, overrides)
	if err != nil {
		return out, err
	}
	n, d, ok := FastMultiplier(rate.CanonicalModel)
	if !found || !ok {
		out.reasons = append(out.reasons, UnpricedReason{Kind: "fast_multiplier_missing", Model: event.Model, Tokens: out.pricedTokens, Detail: "没有已确认的 ChatGPT Codex Fast 额度倍率"})
		out.unpricedTokens += out.pricedTokens
		out.pricedTokens = 0
		out.regularNano, out.cachedNano, out.cacheWriteNano, out.outputNano = 0, 0, 0, 0
		return out, nil
	}
	for _, cost := range []*int64{&out.regularNano, &out.cachedNano, &out.cacheWriteNano, &out.outputNano} {
		before := *cost
		*cost, err = tokenCost(before, 1, n, d)
		if err != nil {
			return out, err
		}
		out.surchargeNano, err = checkedAdd(out.surchargeNano, *cost-before)
		if err != nil {
			return out, err
		}
	}
	return out, nil
}
