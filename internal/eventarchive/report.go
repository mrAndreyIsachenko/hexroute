package eventarchive

import (
	"fmt"
	"sort"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

const (
	// ReportSchema names the shape of one archive report.
	ReportSchema = "hexroute.event-archive.report.v1"
	// ReportSchemaVersion is bumped only for an incompatible change.
	ReportSchemaVersion uint16 = 1
	// MaxRareFindings bounds the ranking. A report listing everything ranks
	// nothing.
	MaxRareFindings = 20
)

// SchemaCount is how many records of one schema the window held.
type SchemaCount struct {
	Schema string `json:"schema"`
	Count  uint32 `json:"count"`
}

// ComponentCount is how many records named one component.
type ComponentCount struct {
	Component string `json:"component"`
	Count     uint32 `json:"count"`
}

// TransitionRun is one state change a component actually made.
type TransitionRun struct {
	Sequence  uint64 `json:"sequence"`
	Component string `json:"component"`
	From      string `json:"from"`
	To        string `json:"to"`
	Reason    string `json:"reason"`
}

// RareFinding is a kind of record that occurred seldom in the window.
//
// Rarity is the ranking because a review is looking for what it has not seen
// before. The common case is what the counts are for.
type RareFinding struct {
	Schema    string `json:"schema"`
	Component string `json:"component"`
	Reason    string `json:"reason,omitempty"`
	Count     uint32 `json:"count"`
	// FirstSequence is where the rarest instance sits, so a reader can go
	// look at it rather than only be told it happened.
	FirstSequence uint64 `json:"first_sequence"`
	// Commentary is only ever added by an optional model pass, and only to a
	// finding this ranking already produced.
	Commentary string `json:"commentary,omitempty"`
}

// Report is a deterministic summary of one window.
//
// Two runs over one archive produce the same bytes. That is what makes the
// digest worth carrying: a report that differed run to run could not be
// compared with last week's, which is the only thing a weekly report is for.
type Report struct {
	Schema  string `json:"schema"`
	Version uint16 `json:"version"`

	Requested Window `json:"requested"`
	Covered   Window `json:"covered"`
	// Shortened says the archive answered about less than was asked for. It
	// is carried rather than derived so a stored report still says it.
	Shortened bool `json:"shortened"`

	Records uint32 `json:"records"`

	BySchema    []SchemaCount    `json:"by_schema"`
	ByComponent []ComponentCount `json:"by_component"`
	Transitions []TransitionRun  `json:"transitions"`
	Rare        []RareFinding    `json:"rare"`

	Digest string `json:"digest"`
}

type rarityKey struct {
	schema    string
	component string
	reason    string
}

// Summarize aggregates a reading into a report.
//
// Every ordering here is total. Counts sort by descending count then by name;
// the rarity ranking sorts by ascending count, then by the sequence of the
// first instance, then by schema, component and reason. Nothing is ordered by
// map iteration, and no tie is left for the runtime to break.
func Summarize(reading Reading) (Report, error) {
	report := Report{
		Schema: ReportSchema, Version: ReportSchemaVersion,
		Requested: reading.Requested, Covered: reading.Covered,
		Shortened: reading.Shortened(),
		Records:   uint32(len(reading.Records)),
		// Empty rather than nil: a report that omitted its sections when a
		// window held nothing would encode differently from one that held
		// nothing to put in them, and the two are the same answer.
		BySchema:    []SchemaCount{},
		ByComponent: []ComponentCount{},
		Transitions: []TransitionRun{},
		Rare:        []RareFinding{},
	}

	bySchema := map[string]uint32{}
	byComponent := map[string]uint32{}
	rarity := map[rarityKey]*RareFinding{}

	for _, record := range reading.Records {
		decoded, err := event.Decode(record.Event)
		if err != nil {
			return Report{}, fmt.Errorf("%w: record %d: %v",
				ErrArchive, record.Sequence, err)
		}
		schema := string(decoded.Schema)
		bySchema[schema]++

		component, reason := describe(decoded.Payload)
		if component != "" {
			byComponent[component]++
		}
		key := rarityKey{schema: schema, component: component, reason: reason}
		if held, ok := rarity[key]; ok {
			held.Count++
		} else {
			rarity[key] = &RareFinding{
				Schema: schema, Component: component, Reason: reason,
				Count: 1, FirstSequence: record.Sequence,
			}
		}

		if transition, ok := decoded.Payload.(*event.Transition); ok {
			report.Transitions = append(report.Transitions, TransitionRun{
				Sequence:  record.Sequence,
				Component: string(transition.Component),
				From:      string(transition.From),
				To:        string(transition.To),
				Reason:    string(transition.Reason),
			})
		}
	}

	for schema, count := range bySchema {
		report.BySchema = append(report.BySchema,
			SchemaCount{Schema: schema, Count: count})
	}
	sort.Slice(report.BySchema, func(one, other int) bool {
		if report.BySchema[one].Count != report.BySchema[other].Count {
			return report.BySchema[one].Count > report.BySchema[other].Count
		}
		return report.BySchema[one].Schema < report.BySchema[other].Schema
	})

	for component, count := range byComponent {
		report.ByComponent = append(report.ByComponent,
			ComponentCount{Component: component, Count: count})
	}
	sort.Slice(report.ByComponent, func(one, other int) bool {
		if report.ByComponent[one].Count != report.ByComponent[other].Count {
			return report.ByComponent[one].Count > report.ByComponent[other].Count
		}
		return report.ByComponent[one].Component < report.ByComponent[other].Component
	})

	ranked := make([]RareFinding, 0, len(rarity))
	for _, finding := range rarity {
		ranked = append(ranked, *finding)
	}
	sort.Slice(ranked, func(one, other int) bool {
		return lessRare(ranked[one], ranked[other])
	})
	if len(ranked) > MaxRareFindings {
		ranked = ranked[:MaxRareFindings]
	}
	report.Rare = ranked

	digest, _, err := policy.CanonicalSHA256(reportBody(report))
	if err != nil {
		return Report{}, fmt.Errorf("%w: %v", ErrArchive, err)
	}
	report.Digest = digest
	return report, nil
}

// lessRare is the documented tie-break: fewer occurrences first, then the
// earlier first instance, then schema, component and reason in that order.
// Every comparison is on a recorded value, so the ranking is a property of the
// window rather than of the run.
func lessRare(one, other RareFinding) bool {
	if one.Count != other.Count {
		return one.Count < other.Count
	}
	if one.FirstSequence != other.FirstSequence {
		return one.FirstSequence < other.FirstSequence
	}
	if one.Schema != other.Schema {
		return one.Schema < other.Schema
	}
	if one.Component != other.Component {
		return one.Component < other.Component
	}
	return one.Reason < other.Reason
}

func reportBody(report Report) Report {
	report.Digest = ""
	// Commentary is never part of the digest. A report has to be comparable
	// with one produced when no model was available, and letting commentary
	// move the digest would make the model's presence look like a change in
	// what happened.
	stripped := make([]RareFinding, len(report.Rare))
	copy(stripped, report.Rare)
	for index := range stripped {
		stripped[index].Commentary = ""
	}
	report.Rare = stripped
	return report
}

// Digested recomputes the digest, so a stored report can be checked against
// its own content.
func (report Report) Digested() (string, error) {
	digest, _, err := policy.CanonicalSHA256(reportBody(report))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrArchive, err)
	}
	return digest, nil
}

// describe names the component and reason a payload is about, where it has
// them. A payload with neither contributes to its schema's count and to
// nothing else, which is correct: it says something happened without saying
// where.
func describe(payload any) (component, reason string) {
	switch value := payload.(type) {
	case *event.Observation:
		return string(value.Component), string(value.Reason)
	case *event.Transition:
		return string(value.Component), string(value.Reason)
	case *event.Incident:
		return string(value.Component), string(value.Category)
	case *event.Diagnostic:
		return string(value.Component), string(value.Code)
	case *event.ArchiveOverflow:
		return string(control.ComponentRuntime), string(value.Reason)
	default:
		return "", ""
	}
}
