// Package schedule calculates deterministic project duration and portfolio impact.
// It deliberately has no database or ML dependencies: callers pass an immutable
// run snapshot, and the same snapshot plus seed always produces the same result.
package schedule

import (
	"fmt"
	"math"
	"math/rand"
	"sort"

	"github.com/teamopt/backend/internal/domain"
)

const (
	defaultRuns = 2000
	hoursPerDay = 8.0
)

// Effort is a three-point estimate in person-days.
type Effort struct {
	Min, Likely, Max float64
}

// Requirement is work that must be completed by the proposed team.
type Requirement struct {
	ID     string
	Effort Effort
}

// Assignment is planned work for one requirement in one ISO week.
type Assignment struct {
	EmployeeID    string
	RequirementID string
	Week          string
	Hours         float64
}

// Capacity is free capacity for an employee in a week. Unavailable weeks do
// not contribute to schedule progress.
type Capacity struct {
	EmployeeID  string
	Week        string
	FreeHours   float64
	Unavailable bool
}

// PortfolioProject describes existing work that can slip when people are
// pulled into the proposed project. RemainingHours is the baseline work left.
type PortfolioProject struct {
	ProjectID      string
	RemainingHours float64
	WeeklyCapacity float64
	Critical       bool
}

// Input is a complete schedule snapshot. Weeks must be in chronological order.
type Input struct {
	Requirements            []Requirement
	Assignments             []Assignment
	Capacity                []Capacity
	Portfolio               []PortfolioProject
	DeadlineWeeks           *float64
	MonteCarloRuns          int
	Seed                    int64
	PortfolioTauWorkingDays float64
}

// Duration is the percentile summary of simulated project completion.
type Duration struct {
	P50Weeks       float64
	P80Weeks       float64
	P50WorkingDays int
	P80WorkingDays int
	POnTime        *float64
	Runs           int
	Seed           int64
}

// PortfolioImpact is the M8 input/output. Slips are deterministic and use the
// same proposed-project load for every simulation-independent calculation.
type PortfolioImpact struct {
	TotalSlipDays    float64
	ProjectsAffected int
	CriticalAffected int
	ProjectSlipDays  map[string]float64
	Score            float64
}

// Result contains the schedule and portfolio calculations for one variant.
type Result struct {
	Duration        Duration
	PortfolioImpact PortfolioImpact
}

// Calculate computes P50/P80 with a triangular distribution and a fixed seed.
// Work advances only on weeks where the corresponding assignment has capacity.
// If no assignment can make progress, the function returns an error instead of
// inventing a duration.
func Calculate(in Input) (Result, error) {
	if len(in.Requirements) == 0 {
		return Result{}, fmt.Errorf("schedule requires at least one requirement")
	}
	runs := in.MonteCarloRuns
	if runs == 0 {
		runs = defaultRuns
	}
	if runs < 1 {
		return Result{}, fmt.Errorf("monte carlo runs must be positive")
	}
	if in.PortfolioTauWorkingDays <= 0 {
		in.PortfolioTauWorkingDays = 10
	}

	effort := make(map[string]Effort, len(in.Requirements))
	for _, r := range in.Requirements {
		if r.ID == "" || r.Effort.Min <= 0 || r.Effort.Min > r.Effort.Likely || r.Effort.Likely > r.Effort.Max {
			return Result{}, fmt.Errorf("invalid effort for requirement %q", r.ID)
		}
		effort[r.ID] = r.Effort
	}
	weeks := make([]string, 0, len(in.Capacity))
	for _, c := range in.Capacity {
		if !contains(weeks, c.Week) {
			weeks = append(weeks, c.Week)
		}
	}
	sort.Strings(weeks)
	if len(weeks) == 0 {
		return Result{}, fmt.Errorf("schedule requires capacity weeks")
	}

	capacity := make(map[string]float64)
	for _, c := range in.Capacity {
		if c.Unavailable || c.FreeHours <= 0 {
			continue
		}
		capacity[c.EmployeeID+"|"+c.Week] += c.FreeHours
	}
	weekly := make(map[string]float64)
	for _, a := range in.Assignments {
		if _, ok := effort[a.RequirementID]; !ok {
			return Result{}, fmt.Errorf("assignment references unknown requirement %q", a.RequirementID)
		}
		if a.Hours <= 0 {
			continue
		}
		free := capacity[a.EmployeeID+"|"+a.Week]
		if free <= 0 {
			continue
		}
		if a.Hours < free {
			free = a.Hours
		}
		weekly[a.RequirementID+"|"+a.Week] += free
	}
	if len(weekly) == 0 {
		return Result{}, fmt.Errorf("schedule has no usable assignment capacity")
	}

	// Requirement ids in a fixed order: the Monte Carlo draws one triangular sample per
	// requirement from a single RNG stream, so iterating a map would tie the samples to
	// Go's randomized map order and make the duration non-reproducible.
	requirementIDs := make([]string, 0, len(effort))
	for id := range effort {
		requirementIDs = append(requirementIDs, id)
	}
	sort.Strings(requirementIDs)

	values := make([]float64, runs)
	onTime := 0
	for i := 0; i < runs; i++ {
		rng := rand.New(rand.NewSource(in.Seed + int64(i)))
		remaining := make(map[string]float64, len(effort))
		for _, id := range requirementIDs {
			e := effort[id]
			remaining[id] = triangular(rng, e.Min, e.Likely, e.Max) * hoursPerDay
		}
		finish := 0.0
		for _, id := range requirementIDs {
			left := remaining[id]
			if left <= 0 {
				continue
			}
			completedAt := float64(len(weeks) + 1)
			for wi, week := range weeks {
				progress := weekly[id+"|"+week]
				if progress <= 0 {
					continue
				}
				if left <= progress {
					completedAt = float64(wi) + left/progress
					break
				}
				left -= progress
			}
			if completedAt > finish {
				finish = completedAt
			}
		}
		values[i] = finish
		if in.DeadlineWeeks != nil && values[i] <= *in.DeadlineWeeks {
			onTime++
		}
	}
	sort.Float64s(values)
	p50 := percentile(values, 0.50)
	p80 := percentile(values, 0.80)
	var probability *float64
	if in.DeadlineWeeks != nil {
		p := float64(onTime) / float64(runs)
		probability = &p
	}
	return Result{Duration: Duration{P50Weeks: p50, P80Weeks: p80, P50WorkingDays: days(p50), P80WorkingDays: days(p80), POnTime: probability, Runs: runs, Seed: in.Seed}, PortfolioImpact: calculatePortfolio(in)}, nil
}

func calculatePortfolio(in Input) PortfolioImpact {
	impact := PortfolioImpact{ProjectSlipDays: map[string]float64{}}
	pulledBySlot := make(map[string]float64)
	freeBySlot := make(map[string]float64)
	for _, c := range in.Capacity {
		if !c.Unavailable && c.FreeHours > 0 {
			freeBySlot[c.EmployeeID+"|"+c.Week] += c.FreeHours
		}
	}
	for _, a := range in.Assignments {
		if a.Hours > 0 {
			pulledBySlot[a.EmployeeID+"|"+a.Week] += a.Hours
		}
	}
	pulled := 0.0
	for slot, hours := range pulledBySlot {
		pulled += math.Min(hours, freeBySlot[slot])
	}
	for _, p := range in.Portfolio {
		if p.RemainingHours <= 0 || p.WeeklyCapacity <= 0 {
			continue
		}
		if pulled <= 0 {
			continue
		}
		slip := pulled / p.WeeklyCapacity * 5
		if slip > 0 {
			impact.ProjectsAffected++
			if p.Critical {
				impact.CriticalAffected++
			}
		}
		impact.ProjectSlipDays[p.ProjectID] = slip
		impact.TotalSlipDays += slip
	}
	tau := in.PortfolioTauWorkingDays
	impact.Score = math.Exp(-impact.TotalSlipDays / tau)
	return impact
}

func triangular(r *rand.Rand, min, likely, max float64) float64 {
	u := r.Float64()
	pivot := (likely - min) / (max - min)
	if u < pivot {
		return min + math.Sqrt(u*(max-min)*(likely-min))
	}
	return max - math.Sqrt((1-u)*(max-min)*(max-likely))
}
func percentile(values []float64, p float64) float64 {
	if len(values) == 1 {
		return values[0]
	}
	pos := p * float64(len(values)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	if lo == hi {
		return values[lo]
	}
	return values[lo] + (values[hi]-values[lo])*(pos-float64(lo))
}
func days(weeks float64) int { return int(math.Ceil(weeks * 5)) }
func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// Keep the domain import part of the package's public integration surface: the
// adapter below makes CapacityService snapshots usable without duplicating data.
func CapacityFromDomain(employeeID string, rows []domain.WeeklyCapacity) []Capacity {
	out := make([]Capacity, 0, len(rows))
	for _, r := range rows {
		out = append(out, Capacity{EmployeeID: employeeID, Week: r.Week, FreeHours: float64(r.AvailableMinutes) / 60, Unavailable: r.AvailableMinutes == 0})
	}
	return out
}
