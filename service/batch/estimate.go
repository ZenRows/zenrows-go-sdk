package batch

import (
	"fmt"
	"strings"
)

// Credits charged per billed request, by configuration. A successful request is billed, and so
// is a target that answers 404 or 410 (the task still ends as failed). mode=auto is charged
// after the fact, anywhere in the Auto range.
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

// String renders the estimate on one line, e.g. "10 credits (2 tasks)", or
// "3-75 credits (3 tasks)" when auto tasks make it a range.
func (e CostEstimate) String() string {
	credits := fmt.Sprintf("%d", e.Min)
	if !e.Exact() {
		credits = fmt.Sprintf("%d-%d", e.Min, e.Max)
	}
	return fmt.Sprintf("%s credits (%d tasks)", credits, e.TaskCount)
}

// AutoTasks is how many tasks use mode=auto — the only source of range.
func (e CostEstimate) AutoTasks() int {
	for _, line := range e.Breakdown {
		if line.Tier == TierAuto {
			return line.Count
		}
	}
	return 0
}

// truthy reports whether a scraper-param value turns a flag on. The Batch API accepts a boolean
// param only as a JSON true or false, or a string equal to "true" or "false" ignoring case; of
// those, only true and "true" turn the flag on. It rejects anything else (1, "1", "yes", "on",
// " true ") with a 400, so those price as off here.
func truthy(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true")
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
// succeeds once. Pure and offline, no network call. Each task's params are the job-level
// zenrowsParams with the task's own ZenRowsParams merged over them (the task wins on a key
// collision), the same merge the API applies.
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
