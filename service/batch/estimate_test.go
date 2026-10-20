package batch_test

import (
	"testing"

	"github.com/zenrows/zenrows-go-sdk/service/batch"
)

func TestEstimateCostBaseTier(t *testing.T) {
	est := batch.EstimateCost([]batch.Task{{URL: "https://a"}, {URL: "https://b"}}, nil)
	if !est.Exact() || est.Min != 2 || est.Max != 2 {
		t.Fatalf("expected 2 base-tier tasks at 1 credit each, got %+v", est)
	}
}

func TestEstimateCostJSRenderTier(t *testing.T) {
	est := batch.EstimateCost([]batch.Task{{URL: "https://a", ZenRowsParams: map[string]any{"js_render": true}}}, nil)
	if est.Min != batch.JSCredits || est.Max != batch.JSCredits {
		t.Fatalf("expected js_render tier at %d credits, got %+v", batch.JSCredits, est)
	}
}

func TestEstimateCostPremiumProxyTier(t *testing.T) {
	est := batch.EstimateCost([]batch.Task{{URL: "https://a", ZenRowsParams: map[string]any{"premium_proxy": true}}}, nil)
	if est.Min != batch.PremiumProxyCredits || est.Max != batch.PremiumProxyCredits {
		t.Fatalf("expected premium_proxy tier at %d credits, got %+v", batch.PremiumProxyCredits, est)
	}
}

func TestEstimateCostJSAndPremiumTier(t *testing.T) {
	est := batch.EstimateCost([]batch.Task{{URL: "https://a", ZenRowsParams: map[string]any{"js_render": true, "premium_proxy": true}}}, nil)
	if est.Min != batch.JSAndProxyCredits || est.Max != batch.JSAndProxyCredits {
		t.Fatalf("expected js_render+premium_proxy tier at %d credits, got %+v", batch.JSAndProxyCredits, est)
	}
}

func TestEstimateCostAutoModeTakesPriorityAndIsARange(t *testing.T) {
	// mode=auto wins even if js_render/premium_proxy are also (incorrectly) set, matching
	// what the engine would honor — and it's the only tier that's a real range, not exact.
	est := batch.EstimateCost([]batch.Task{{URL: "https://a", ZenRowsParams: map[string]any{
		"mode": "auto", "js_render": true, "premium_proxy": true,
	}}}, nil)
	if est.Exact() {
		t.Fatalf("expected an inexact (ranged) estimate for an auto task, got %+v", est)
	}
	if est.Min != batch.AutoMinCredits || est.Max != batch.AutoMaxCredits {
		t.Fatalf("expected the auto range %d-%d, got %+v", batch.AutoMinCredits, batch.AutoMaxCredits, est)
	}
	if est.AutoTasks() != 1 {
		t.Fatalf("expected AutoTasks()==1, got %d", est.AutoTasks())
	}
}

func TestEstimateCostModeAutoIsCaseInsensitiveAndTrimmed(t *testing.T) {
	est := batch.EstimateCost([]batch.Task{{URL: "https://a", ZenRowsParams: map[string]any{"mode": " AUTO "}}}, nil)
	if est.Exact() {
		t.Fatalf("expected mode=' AUTO ' to be recognized as auto, got %+v", est)
	}
}

func TestEstimateCostTaskParamsOverrideJobParamsOnCollision(t *testing.T) {
	// Job-level zenrows_params says js_render=false; the task overrides to true — task wins,
	// matching the worker's merge semantics.
	est := batch.EstimateCost(
		[]batch.Task{{URL: "https://a", ZenRowsParams: map[string]any{"js_render": true}}},
		map[string]any{"js_render": false},
	)
	if est.Min != batch.JSCredits {
		t.Fatalf("expected task-level js_render:true to win over job-level js_render:false, got %+v", est)
	}
}

func TestEstimateCostTruthyMatchesTheAPI(t *testing.T) {
	// The API accepts a JSON true/false or "true"/"false" in any case for a boolean param, and
	// only true and "true" turn it on. Anything else is a 400 from the API, so it must not
	// price as on.
	cases := []struct {
		value any
		want  int
	}{
		{true, batch.JSCredits},
		{"true", batch.JSCredits},
		{"TRUE", batch.JSCredits},
		{"True", batch.JSCredits},
		{false, batch.BaseCredits},
		{"false", batch.BaseCredits},
		{"FALSE", batch.BaseCredits},
		{"False", batch.BaseCredits},
		{1, batch.BaseCredits},
		{1.0, batch.BaseCredits},
		{"1", batch.BaseCredits},
		{"yes", batch.BaseCredits},
		{"on", batch.BaseCredits},
		{" true ", batch.BaseCredits},
	}
	for _, tc := range cases {
		est := batch.EstimateCost([]batch.Task{{URL: "https://a", ZenRowsParams: map[string]any{"js_render": tc.value}}}, nil)
		if est.Min != tc.want {
			t.Fatalf("js_render=%#v: got %d credits, want %d", tc.value, est.Min, tc.want)
		}
	}
}

func TestEstimateCostFalsyValuesFallBackToBase(t *testing.T) {
	for _, v := range []any{false, "false", "0", 0, ""} {
		est := batch.EstimateCost([]batch.Task{{URL: "https://a", ZenRowsParams: map[string]any{"js_render": v}}}, nil)
		if est.Min != batch.BaseCredits {
			t.Fatalf("expected js_render=%v to be falsy (base tier), got %+v", v, est)
		}
	}
}

func TestEstimateCostBreakdownIsAggregatedAndOrdered(t *testing.T) {
	est := batch.EstimateCost([]batch.Task{
		{URL: "https://a"},
		{URL: "https://b"},
		{URL: "https://c", ZenRowsParams: map[string]any{"js_render": true}},
		{URL: "https://d", ZenRowsParams: map[string]any{"mode": "auto"}},
	}, nil)
	if len(est.Breakdown) != 3 {
		t.Fatalf("expected 3 breakdown lines (base x2, js x1, auto x1 aggregated into 3 tiers), got %+v", est.Breakdown)
	}
	if est.Breakdown[0].Tier != batch.TierBase || est.Breakdown[0].Count != 2 {
		t.Fatalf("expected base tier first with count 2, got %+v", est.Breakdown[0])
	}
	last := est.Breakdown[len(est.Breakdown)-1]
	if last.Tier != batch.TierAuto {
		t.Fatalf("expected auto tier last (stable render order), got %+v", est.Breakdown)
	}
}

func TestEstimateCostEmptyTasksIsZero(t *testing.T) {
	est := batch.EstimateCost(nil, nil)
	if est.TaskCount != 0 || est.Min != 0 || est.Max != 0 || !est.Exact() {
		t.Fatalf("expected a zero estimate for no tasks, got %+v", est)
	}
}

func TestCostLineSubtotalsAndExact(t *testing.T) {
	line := batch.CostLine{Tier: batch.TierJS, Count: 3, UnitMin: 5, UnitMax: 5}
	if line.SubtotalMin() != 15 || line.SubtotalMax() != 15 || !line.Exact() {
		t.Fatalf("unexpected subtotal/exact computation: %+v", line)
	}
}
