package store

import (
	"sort"
	"time"

	"github.com/Verhum/burnrate/internal/domain"
)

// analyticsRunLimit caps how many runs the cost-efficiency aggregation reads.
// It matches the leaderboard's own cap; the table is one row per run attempt, so
// this is years of history rather than a real truncation.
const analyticsRunLimit = 10000

// CostEfficiency answers "what did a task cost, and what did a line of code
// cost", grouped by model and bucketed by UTC day over the last `days` days.
//
// Both figures are ratios over the bucket rather than averages of per-run
// ratios: cost divided by tasks, and cost divided by lines. That makes a
// rate-limited attempt that spent money without landing lines raise the day's
// unit cost — which is the honest answer — instead of vanishing from the
// numerator.
//
// A task that spans two days or two models is counted in each bucket it was
// worked in, so summing the Tasks column across buckets can exceed the number of
// distinct tasks. Per-bucket cost is never double-counted, because cost lives on
// the run and every run falls in exactly one bucket.
func (s *Store) CostEfficiency(days int) (*domain.CostEfficiency, error) {
	if days <= 0 {
		days = 30
	}

	runs, err := s.ListRuns(0, analyticsRunLimit)
	if err != nil {
		return nil, err
	}

	// Series order is computed over all of history, not over the requested
	// range: the frontend picks a palette slot by index, and changing the range
	// must not repaint the models that survive the narrower filter.
	models := modelOrder(runs)

	cutoff := time.Now().UTC().AddDate(0, 0, -(days - 1)).Format("2006-01-02")
	points := aggregateCostEfficiency(runs, cutoff)

	return &domain.CostEfficiency{
		Days:   days,
		Models: models,
		Points: points,
		Totals: totalCostEfficiency(points),
	}, nil
}

// runModel is the grouping key for a run: its recorded model, or UnknownModel
// for the runs that predate the column.
func runModel(r domain.Run) string {
	if r.Model == "" {
		return domain.UnknownModel
	}
	return r.Model
}

// modelOrder lists every model ever run, oldest first by the run that
// introduced it. ListRuns returns newest-first, so it is walked backwards.
func modelOrder(runs []domain.Run) []string {
	var order []string
	seen := map[string]bool{}
	for i := len(runs) - 1; i >= 0; i-- {
		m := runModel(runs[i])
		if seen[m] {
			continue
		}
		seen[m] = true
		order = append(order, m)
	}
	return order
}

// aggregateCostEfficiency buckets runs by (UTC day, model), dropping runs that
// neither started nor cost anything. Output is sorted by date then by model so
// the response is stable across calls.
func aggregateCostEfficiency(runs []domain.Run, cutoff string) []domain.CostEfficiencyPoint {
	type key struct {
		date  string
		model string
	}
	buckets := map[key]*domain.CostEfficiencyPoint{}
	tasksSeen := map[key]map[int64]bool{}

	for _, r := range runs {
		if len(r.StartedAt) < 10 {
			continue
		}
		day := r.StartedAt[:10]
		if day < cutoff {
			continue
		}
		// A run with neither spend nor churn says nothing about unit cost, and
		// including it would only depress cost-per-task with empty attempts.
		if r.CostUSD <= 0 && r.LinesChanged() == 0 {
			continue
		}

		k := key{date: day, model: runModel(r)}
		p := buckets[k]
		if p == nil {
			p = &domain.CostEfficiencyPoint{Date: k.date, Model: k.model}
			buckets[k] = p
			tasksSeen[k] = map[int64]bool{}
		}
		p.Runs++
		p.CostUSD += r.CostUSD
		p.LinesAdded += r.LinesAdded
		p.LinesRemoved += r.LinesRemoved
		tasksSeen[k][r.TaskID] = true
	}

	out := make([]domain.CostEfficiencyPoint, 0, len(buckets))
	for k, p := range buckets {
		p.Tasks = len(tasksSeen[k])
		finalizeRatios(p)
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date < out[j].Date
		}
		return out[i].Model < out[j].Model
	})
	return out
}

// totalCostEfficiency rolls the whole range up to one row per model, for the
// legend's summary figures. Date is left empty to mark the row as a total.
func totalCostEfficiency(points []domain.CostEfficiencyPoint) []domain.CostEfficiencyPoint {
	totals := map[string]*domain.CostEfficiencyPoint{}
	for _, p := range points {
		t := totals[p.Model]
		if t == nil {
			t = &domain.CostEfficiencyPoint{Model: p.Model}
			totals[p.Model] = t
		}
		t.Runs += p.Runs
		// Summing per-day task counts, so a task worked on two days counts
		// twice. That keeps the total's cost-per-task consistent with the
		// per-day figures it summarizes.
		t.Tasks += p.Tasks
		t.CostUSD += p.CostUSD
		t.LinesAdded += p.LinesAdded
		t.LinesRemoved += p.LinesRemoved
	}

	out := make([]domain.CostEfficiencyPoint, 0, len(totals))
	for _, t := range totals {
		finalizeRatios(t)
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Model < out[j].Model })
	return out
}

// finalizeRatios fills the derived columns. A zero denominator leaves the ratio
// at zero, which the UI reads as "no data" and renders as a gap rather than as a
// point on the axis.
func finalizeRatios(p *domain.CostEfficiencyPoint) {
	p.LinesChanged = p.LinesAdded + p.LinesRemoved
	if p.Tasks > 0 {
		p.CostPerTask = p.CostUSD / float64(p.Tasks)
	}
	if p.LinesChanged > 0 {
		p.CostPerLine = p.CostUSD / float64(p.LinesChanged)
	}
}
