// Package soakcompare judges whether this runtime's decision rule reproduces the
// runtime that owns the tunnel.
//
// The judgement decides whether a runtime is given authority over the network,
// so it is a program with tests rather than a reading of two records side by
// side. It pairs this runtime's rebuild decisions with the owner's rebuilds by
// cause, inside a window, and says which agreed, which disagreed in each
// direction, and which agreements were on events the operator induced.
package soakcompare

import (
	"errors"
	"sort"
	"time"
)

// The causes the rule acts on, named as this runtime's records name them.
const (
	ProcessGone    = "process_gone"
	WakeGap        = "wake_gap"
	CarrierChanged = "carrier_changed"
)

// Causes is every cause judged, in the order they are reported.
var Causes = []string{ProcessGone, WakeGap, CarrierChanged}

// Decision is one of this runtime's decisions to rebuild.
type Decision struct {
	At     time.Time
	Causes []string
}

// Rebuild is one rebuild the owning runtime made, for one cause.
type Rebuild struct {
	At    time.Time
	Cause string
}

// Induction is the operator's own record that they caused an event.
type Induction struct {
	At    time.Time
	Cause string
}

// Criterion is what a soak has to show to pass.
type Criterion struct {
	Days             int
	NaturalWakeGaps  int
	CarrierChanges   int
	InducedProcesses int
}

// Pass is the criterion the grill of item 9 settled.
var Pass = Criterion{Days: 7, NaturalWakeGaps: 3, CarrierChanges: 2, InducedProcesses: 1}

// Episode is one or more decisions for a cause close enough together to be one
// condition held across cycles. The owner rebuilds once for it, and this runtime
// decides again on every cycle until it passes, so the comparison is between an
// episode and a rebuild rather than between a decision and a rebuild.
type Episode struct {
	Cause     string
	First     time.Time
	Last      time.Time
	Decisions int
}

// Result is the judgement for one cause.
type Result struct {
	Cause string
	// Agreements are rebuilds matched to an episode; Induced counts those whose
	// event the operator recorded causing.
	Agreements int
	Induced    int
	// DecidedNotMade are episodes the owner made no rebuild for.
	DecidedNotMade []Episode
	// MadeNotDecided are rebuilds no episode was decided for.
	MadeNotDecided []Rebuild
}

// Natural is the agreements that were not induced.
func (result Result) Natural() int { return result.Agreements - result.Induced }

// Report is the judgement over a whole soak.
type Report struct {
	From, Until time.Time
	Results     map[string]Result
}

var ErrInvalidSoak = errors.New("invalid soak comparison")

// Compare pairs decisions with rebuilds inside the window.
//
// Decisions for a cause are grouped into episodes when consecutive ones are no
// further apart than the window. An episode agrees with a rebuild for the same
// cause when the rebuild falls within the window of the episode's first or last
// decision; each rebuild agrees with at most one episode and each episode with at
// most one rebuild. An agreement is induced when an induction for the same cause
// falls within the window of the rebuild.
func Compare(decisions []Decision, rebuilds []Rebuild, inductions []Induction, window time.Duration, from, until time.Time) (Report, error) {
	if window <= 0 || !until.After(from) {
		return Report{}, ErrInvalidSoak
	}
	report := Report{From: from, Until: until, Results: map[string]Result{}}
	for _, cause := range Causes {
		episodes := episodesFor(decisions, cause, window, from, until)
		made := rebuildsFor(rebuilds, cause, from, until)
		result := Result{Cause: cause}
		matchedRebuild := make([]bool, len(made))
		for _, episode := range episodes {
			best := -1
			for index, rebuild := range made {
				if matchedRebuild[index] || !within(episode, rebuild.At, window) {
					continue
				}
				if best < 0 || distance(episode, rebuild.At) < distance(episode, made[best].At) {
					best = index
				}
			}
			if best < 0 {
				result.DecidedNotMade = append(result.DecidedNotMade, episode)
				continue
			}
			matchedRebuild[best] = true
			result.Agreements++
			if induced(inductions, cause, made[best].At, window) {
				result.Induced++
			}
		}
		for index, rebuild := range made {
			if !matchedRebuild[index] {
				result.MadeNotDecided = append(result.MadeNotDecided, rebuild)
			}
		}
		report.Results[cause] = result
	}
	return report, nil
}

// Disagreements is how many there were, in both directions, across causes.
func (report Report) Disagreements() int {
	total := 0
	for _, result := range report.Results {
		total += len(result.DecidedNotMade) + len(result.MadeNotDecided)
	}
	return total
}

// Passes answers whether the soak meets the criterion, and says what is missing
// when it does not.
func (report Report) Passes(criterion Criterion) (bool, []string) {
	var missing []string
	if days := report.Until.Sub(report.From); days < time.Duration(criterion.Days)*24*time.Hour {
		missing = append(missing, "the soak is shorter than the required days")
	}
	if report.Disagreements() > 0 {
		missing = append(missing, "there are disagreements")
	}
	if report.Results[WakeGap].Natural() < criterion.NaturalWakeGaps {
		missing = append(missing, "too few natural wake-gap agreements")
	}
	if report.Results[CarrierChanged].Agreements < criterion.CarrierChanges {
		missing = append(missing, "too few carrier agreements")
	}
	if report.Results[ProcessGone].Induced < criterion.InducedProcesses {
		missing = append(missing, "no induced process loss agreed")
	}
	return len(missing) == 0, missing
}

func episodesFor(decisions []Decision, cause string, window time.Duration, from, until time.Time) []Episode {
	var times []time.Time
	for _, decision := range decisions {
		if decision.At.Before(from) || !decision.At.Before(until) {
			continue
		}
		for _, named := range decision.Causes {
			if named == cause {
				times = append(times, decision.At)
				break
			}
		}
	}
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })
	var episodes []Episode
	for _, at := range times {
		if count := len(episodes); count > 0 && at.Sub(episodes[count-1].Last) <= window {
			episodes[count-1].Last = at
			episodes[count-1].Decisions++
			continue
		}
		episodes = append(episodes, Episode{Cause: cause, First: at, Last: at, Decisions: 1})
	}
	return episodes
}

func rebuildsFor(rebuilds []Rebuild, cause string, from, until time.Time) []Rebuild {
	var made []Rebuild
	for _, rebuild := range rebuilds {
		if rebuild.Cause == cause && !rebuild.At.Before(from) && rebuild.At.Before(until) {
			made = append(made, rebuild)
		}
	}
	sort.Slice(made, func(i, j int) bool { return made[i].At.Before(made[j].At) })
	return made
}

func within(episode Episode, at time.Time, window time.Duration) bool {
	return !at.Before(episode.First.Add(-window)) && !at.After(episode.Last.Add(window))
}

func distance(episode Episode, at time.Time) time.Duration {
	if at.Before(episode.First) {
		return episode.First.Sub(at)
	}
	if at.After(episode.Last) {
		return at.Sub(episode.Last)
	}
	return 0
}

func induced(inductions []Induction, cause string, at time.Time, window time.Duration) bool {
	for _, induction := range inductions {
		if induction.Cause != cause {
			continue
		}
		if gap := at.Sub(induction.At); gap >= -window && gap <= window {
			return true
		}
	}
	return false
}
