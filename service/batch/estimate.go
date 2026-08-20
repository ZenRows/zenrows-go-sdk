package batch

import "strings"

// Credits charged per successful request, by configuration. mode=auto is charged
// dynamically post-factum, anywhere in the Auto range.
const (
	BaseCredits         = 1
	JSCredits           = 5
	PremiumProxyCredits = 10
	JSAndProxyCredits   = 25
	AutoMinCredits      = 1
	AutoMaxCredits      = 25
)

// Tier is the pricing tier a task falls into. Exactly one per task.
type Tier string

const (
	TierBase         Tier = "base"
	TierJS           Tier = "js_render"
	TierPremium      Tier = "premium_proxy"
	TierJSAndPremium Tier = "js_render+premium_proxy"
	TierAuto         Tier = "auto"
)

// tierOrder is the stable render order for a CostEstimate's breakdown.
var tierOrder = []Tier{TierBase, TierJS, TierPremium, TierJSAndPremium, TierAuto}

// CostLine is one row of a CostEstimate's breakdown: all tasks sharing a tier, aggregated.
type CostLine struct {
	Tier    Tier
	Count   int
	UnitMin int
	UnitMax int
}

// SubtotalMin is Count * UnitMin.
func (l CostLine) SubtotalMin() int { return l.Count * l.UnitMin }

// SubtotalMax is Count * UnitMax.
func (l CostLine) SubtotalMax() int { return l.Count * l.UnitMax }

// Exact is true when UnitMin == UnitMax (every task in this tier prices identically).
func (l CostLine) Exact() bool { return l.UnitMin == l.UnitMax }

// CostEstimate is the result of Client.EstimateCost — credits assuming every task succeeds
// once. Exact() is true when no task uses mode=auto.
type CostEstimate struct {
	TaskCount int
	Min       int
	Max       int
	Breakdown []CostLine
}

// Exact is true when the charge is a single number (no auto tasks).
func (e CostEstimate) Exact() bool { return e.Min == e.Max }

// AutoTasks is how many tasks use mode=auto — the only source of range.
func (e CostEstimate) AutoTasks() int {
	for _, line := range e.Breakdown {
		if line.Tier == TierAuto {
			return line.Count
		}
	}
	return 0
}

var truthyStrings = map[string]bool{"true": true, "1": true, "yes": true, "on": true}

// truthy coerces a scraper-param scalar to a boolean the same way the server's billing engine
// does: bools pass through, numbers are nonzero-truthy, strings match the on-spellings
// (case-insensitive).
func truthy(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case int:
		return v != 0
	case int64:
		return v != 0
	case float64:
		return v != 0
	case string:
		return truthyStrings[strings.ToLower(strings.TrimSpace(v))]
	default:
		return false
	}
}

func isAutoMode(params map[string]any) bool {
	v, ok := params["mode"]
	if !ok {
		return false
	}
	s, ok := v.(string)
	if !ok {
		return false
	}
	return strings.ToLower(strings.TrimSpace(s)) == "auto"
}

func costForParams(params map[string]any) (tier Tier, unitMin, unitMax int) {
	if isAutoMode(params) {
		return TierAuto, AutoMinCredits, AutoMaxCredits
	}
	js := truthy(params["js_render"])
	px := truthy(params["premium_proxy"])
	switch {
	case js && px:
		return TierJSAndPremium, JSAndProxyCredits, JSAndProxyCredits
	case px:
		return TierPremium, PremiumProxyCredits, PremiumProxyCredits
	case js:
		return TierJS, JSCredits, JSCredits
	default:
		return TierBase, BaseCredits, BaseCredits
	}
}

func taskParams(task Task) map[string]any {
	if task.ZenRowsParams == nil {
		return map[string]any{}
	}
	return task.ZenRowsParams
}

// EstimateCost estimates the credit cost of a job before submitting it, assuming every task
// succeeds once. Pure and offline — no network call. Per-task zenrows_params override the
// job-level zenrows_params on key collision (task wins), matching the worker's merge.
//
// file_input_id-based jobs estimate as zero tasks (Tasks is empty) — the CSV row count isn't
// known client-side.
func EstimateCost(tasks []Task, zenrowsParams map[string]any) CostEstimate {
	agg := map[Tier]*CostLine{}
	totalMin, totalMax, count := 0, 0, 0

	for _, task := range tasks {
		count++
		merged := map[string]any{}
		for k, v := range zenrowsParams {
			merged[k] = v
		}
		for k, v := range taskParams(task) {
			merged[k] = v
		}
		tier, unitMin, unitMax := costForParams(merged)
		totalMin += unitMin
		totalMax += unitMax
		if line, ok := agg[tier]; ok {
			line.Count++
		} else {
			agg[tier] = &CostLine{Tier: tier, Count: 1, UnitMin: unitMin, UnitMax: unitMax}
		}
	}

	breakdown := make([]CostLine, 0, len(agg))
	for _, tier := range tierOrder {
		if line, ok := agg[tier]; ok {
			breakdown = append(breakdown, *line)
		}
	}

	return CostEstimate{TaskCount: count, Min: totalMin, Max: totalMax, Breakdown: breakdown}
}
