// Package soakcli is the command that collects a soak and judges it.
package soakcli

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/eventarchive"
	"github.com/mrAndreyIsachenko/hexroute/internal/soakcompare"
	"github.com/mrAndreyIsachenko/hexroute/internal/soakledger"
)

const (
	DefaultArchive  = "/Library/Application Support/Hexroute/observe-root/state/event-archive"
	DefaultLedger   = "/Library/Application Support/Hexroute/observe-root/state/soak"
	DefaultTwilight = "/Library/Logs/twilight/twilight-events.jsonl"
	Window          = 120 * time.Second
)

const usage = "usage: hexroute-soak-compare [flags] collect|note|judge"

// twilightReasons maps the owning runtime's rebuild transitions to the causes
// this runtime's records name. Other transitions are not rebuilds of this kind.
var twilightReasons = map[string]string{
	"process_missing":   soakcompare.ProcessGone,
	"wake_gap_detected": soakcompare.WakeGap,
	"carrier_changed":   soakcompare.CarrierChanged,
}

func Run(args []string, stdout, stderr io.Writer, now func() time.Time) int {
	if now == nil {
		now = time.Now
	}
	flags := flag.NewFlagSet("hexroute-soak-compare", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	archivePath := flags.String("archive", DefaultArchive, "the root runtime's event archive")
	ledgerPath := flags.String("ledger", DefaultLedger, "where the soak is collected")
	twilightPath := flags.String("twilight", DefaultTwilight, "the owning runtime's event log")
	fromFlag := flags.String("from", "", "the soak's start, RFC 3339")
	untilFlag := flags.String("until", "", "the soak's end, RFC 3339; now when absent")
	cause := flags.String("cause", "", "the cause induced, for note")
	if flags.Parse(args) != nil || flags.NArg() != 1 {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	ledger, err := soakledger.Open(*ledgerPath)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}

	switch flags.Arg(0) {
	case "note":
		if err := ledger.Note(now(), *cause); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 2
		}
		fmt.Fprintf(stdout, "noted %s at %s\n", *cause, now().UTC().Format(time.RFC3339))
		return 0

	case "collect":
		from, held, err := ledger.Next()
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 2
		}
		// --from always wins. The first collection needs it, and collecting
		// again from the soak's start is how windows collected before their
		// silences were kept get measured.
		if *fromFlag != "" || !held {
			if from, err = time.Parse(time.RFC3339, *fromFlag); err != nil {
				fmt.Fprintln(stderr, "error: the first collection needs --from")
				return 2
			}
		}
		archive, err := eventarchive.OpenForReading(*archivePath)
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 2
		}
		until := now().UTC()
		reading, err := archive.Read(eventarchive.Query{From: from, To: until})
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 2
		}
		// A read the limit cut short covers less than it says; collecting it
		// would record a window as observed that was not read.
		if reading.Truncated {
			fmt.Fprintln(stderr, "error: the read was truncated; collect more often")
			return 2
		}
		// Nothing yet is not a hole. A collection run moments after the last, or
		// straight after the soak's start, reads an empty window, and recording
		// it would make the judgement refuse a soak for reading nothing where
		// there was nothing to read yet. An empty window longer than a few cycles
		// is different: the runtime writes several records every cycle, so that
		// silence is a stretch nobody observed, and it is recorded as one.
		if reading.Covered.Empty && until.Sub(from) <= soakledger.MaxLead {
			fmt.Fprintf(stdout, "nothing to collect yet since %s\n", from.Format(time.RFC3339))
			return 0
		}
		entries, err := RebuildEntries(reading.Records)
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 2
		}
		coverage, err := CoverageOf(from, until, reading)
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 2
		}
		if err := ledger.Collect(entries, coverage); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 2
		}
		fmt.Fprintf(stdout, "collected %d records from %s to %s; %d rebuild decisions\n",
			reading.Covered.Records, from.Format(time.RFC3339), coverage.Newest.Format(time.RFC3339), len(entries))
		return 0

	case "judge":
		from, err := time.Parse(time.RFC3339, *fromFlag)
		if err != nil {
			fmt.Fprintln(stderr, "error: judge needs --from")
			return 2
		}
		until := now().UTC()
		if *untilFlag != "" {
			if until, err = time.Parse(time.RFC3339, *untilFlag); err != nil {
				fmt.Fprintln(stderr, "error: --until is not RFC 3339")
				return 2
			}
		}
		return judge(stdout, stderr, ledger, *twilightPath, from, until)

	default:
		fmt.Fprintln(stderr, usage)
		return 2
	}
}

func judge(stdout, stderr io.Writer, ledger *soakledger.Ledger, twilightPath string, from, until time.Time) int {
	windows, err := ledger.Coverage()
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	entries, err := ledger.Decisions()
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	// Every decision, because each says when the cycle before it ran: a silence
	// with a cycle inside it is one whose record was lost rather than one
	// nobody observed. The wakes among them also account for the silences they
	// were decided on.
	dozing := make([]soakcompare.Dozing, 0)
	for _, window := range windows {
		for _, span := range window.Dozing {
			dozing = append(dozing, soakcompare.Dozing{From: span.From, To: span.To})
		}
	}
	cycles := make([]soakledger.Cycle, 0, len(entries))
	for _, entry := range entries {
		cycle := soakledger.Cycle{At: entry.At, TickGap: entry.TickGap}
		for _, cause := range entry.Causes {
			if cause == soakcompare.WakeGap {
				cycle.Wake = true
			}
		}
		cycles = append(cycles, cycle)
	}
	if err := soakledger.Continuous(windows, cycles, from, until); err != nil {
		fmt.Fprintf(stdout, "NOT JUDGEABLE  %v\n", err)
		return 1
	}
	decisions := make([]soakcompare.Decision, 0, len(entries))
	for _, entry := range entries {
		decision := soakcompare.Decision{At: entry.At, Causes: entry.Causes}
		if entry.TickGap > 0 {
			decision.Previous = entry.At.Add(-entry.TickGap)
		}
		decisions = append(decisions, decision)
	}
	rebuilds, restarts, err := TwilightLog(twilightPath)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	inductions, err := ledger.Inductions()
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	report, err := soakcompare.Compare(decisions, rebuilds, inductions, restarts, dozing, Window, from, until)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	fmt.Fprintf(stdout, "soak %s .. %s (%s)\n", from.Format(time.RFC3339), until.Format(time.RFC3339), until.Sub(from).Round(time.Minute))
	for _, cause := range soakcompare.Causes {
		result := report.Results[cause]
		fmt.Fprintf(stdout, "  %-16s agreed %d (natural %d, induced %d)  decided-not-made %d  made-not-decided %d\n",
			cause, result.Agreements, result.Natural(), result.Induced, len(result.DecidedNotMade), len(result.MadeNotDecided))
		for _, episode := range result.DecidedNotMade {
			fmt.Fprintf(stdout, "      decided, not made: %s .. %s (%d decisions)\n",
				episode.First.Format(time.RFC3339), episode.Last.Format(time.RFC3339), episode.Decisions)
		}
		for _, rebuild := range result.MadeNotDecided {
			fmt.Fprintf(stdout, "      made, not decided: %s\n", rebuild.At.Format(time.RFC3339))
		}
		for _, episode := range result.Explained {
			fmt.Fprintf(stdout, "      explained by the owner's own restart: %s .. %s (%d decisions)\n",
				episode.First.Format(time.RFC3339), episode.Last.Format(time.RFC3339), episode.Decisions)
		}
	}
	passed, missing := report.Passes(soakcompare.Pass)
	if !passed {
		fmt.Fprintf(stdout, "NOT PASSED  %s\n", strings.Join(missing, "; "))
		return 1
	}
	fmt.Fprintln(stdout, "PASSED")
	return 0
}

// DozingSpans are the stretches where this runtime's cycles did not finish.
//
// A machine dozing on battery wakes for seconds: the cycle sees the tunnel and
// the carrier, stops before the probes, and records an incomplete decision. Both
// runtimes decide in such a stretch, at moments neither shares, so nothing in it
// is compared. A stretch runs from an incomplete decision to the next complete
// one, which is the first cycle that finished after the machine came back.
func DozingSpans(records []eventarchive.Record) ([]soakledger.Span, error) {
	var spans []soakledger.Span
	open := false
	var from, last time.Time
	for _, record := range records {
		decoded, err := event.Decode(record.Event)
		if err != nil {
			return nil, fmt.Errorf("record %d: %w", record.Sequence, err)
		}
		if decoded.Schema != event.SchemaTunnelDecision {
			continue
		}
		var decision event.TunnelDecision
		switch payload := decoded.Payload.(type) {
		case *event.TunnelDecision:
			decision = *payload
		case event.TunnelDecision:
			decision = payload
		default:
			return nil, fmt.Errorf("record %d: a tunnel decision of type %T", record.Sequence, decoded.Payload)
		}
		at := record.Metadata.WallClock.UTC()
		complete := decision.Grounds != nil && decision.Grounds.Complete
		if !complete {
			if !open {
				open, from = true, at
			}
			last = at
			continue
		}
		if open {
			spans = append(spans, soakledger.Span{From: from, To: at})
			open = false
		}
	}
	if open {
		spans = append(spans, soakledger.Span{From: from, To: last})
	}
	return spans, nil
}

// RebuildEntries is this runtime's decisions to rebuild, out of archive records.
func RebuildEntries(records []eventarchive.Record) ([]soakledger.Entry, error) {
	var entries []soakledger.Entry
	for _, record := range records {
		decoded, err := event.Decode(record.Event)
		if err != nil {
			return nil, fmt.Errorf("record %d: %w", record.Sequence, err)
		}
		if decoded.Schema != event.SchemaTunnelDecision {
			continue
		}
		var decision event.TunnelDecision
		switch payload := decoded.Payload.(type) {
		case *event.TunnelDecision:
			decision = *payload
		case event.TunnelDecision:
			decision = payload
		default:
			return nil, fmt.Errorf("record %d: a tunnel decision of type %T", record.Sequence, decoded.Payload)
		}
		if decision.Action != "rebuild_tunnel" {
			continue
		}
		entry := soakledger.Entry{
			Sequence: record.Sequence, At: record.Metadata.WallClock.UTC(), Causes: decision.Causes,
		}
		if decision.Grounds != nil && decision.Grounds.TickGapMS != nil {
			entry.TickGap = time.Duration(*decision.Grounds.TickGapMS) * time.Millisecond
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// TwilightRebuilds is the owning runtime's rebuilds of the three judged kinds,
// read from its event log. That log is not evicted, so it is read whole at
// judgement rather than collected.
func TwilightRebuilds(path string) ([]soakcompare.Rebuild, error) {
	rebuilds, _, err := TwilightLog(path)
	return rebuilds, err
}

// TwilightLog is the owning runtime's rebuilds of the three judged kinds and
// every restart of its tunnel, whatever the reason. A restart replaces the
// process, and a runtime watching from outside sees that as the process going.
func TwilightLog(path string) ([]soakcompare.Rebuild, []time.Time, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	var rebuilds []soakcompare.Rebuild
	var restarts []time.Time
	// When that runtime last wrote anything, which is the closest its log comes
	// to saying when it last ran.
	var wrote time.Time
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lines := 0
	for scanner.Scan() {
		lines++
		var transition struct {
			Timestamp string `json:"timestamp"`
			From      string `json:"from"`
			To        string `json:"to"`
			Event     string `json:"event"`
			Reason    string `json:"reason"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &transition); err != nil {
			return nil, nil, fmt.Errorf("%s line %d: %w", path, lines, err)
		}
		// Every line says the runtime was there, whatever it says besides, so
		// the time is taken from all of them: what is wanted is when it last
		// ran, not when it last did something judged.
		at, err := time.Parse(time.RFC3339, transition.Timestamp)
		if err != nil {
			return nil, nil, fmt.Errorf("%s line %d: %w", path, lines, err)
		}
		previous := wrote
		wrote = at.UTC()
		if transition.Event != "" || transition.From == "" {
			continue
		}
		cause, judged := twilightReasons[transition.Reason]
		restart := transition.To == "STARTING" || transition.To == "SINGBOX_EXITED"
		if restart {
			restarts = append(restarts, at.UTC())
		}
		if judged {
			rebuilds = append(rebuilds, soakcompare.Rebuild{
				At: at.UTC(), Cause: cause, Previous: previous})
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, err
	}
	if lines == 0 {
		return nil, nil, errors.New("the owning runtime's event log is empty; a judgement against it would be about nothing")
	}
	sort.Slice(rebuilds, func(i, j int) bool { return rebuilds[i].At.Before(rebuilds[j].At) })
	sort.Slice(restarts, func(i, j int) bool { return restarts[i].Before(restarts[j]) })
	return rebuilds, restarts, nil
}

// CoverageOf is what one collection can say it observed.
func CoverageOf(from, until time.Time, reading eventarchive.Reading) (soakledger.Coverage, error) {
	moments := make([]time.Time, 0, len(reading.Records))
	for _, record := range reading.Records {
		moments = append(moments, record.Metadata.WallClock.UTC())
	}
	longest := soakledger.LongestSilence(moments)
	coverage := soakledger.Coverage{
		Requested: from, Records: reading.Covered.Records, CollectedAt: until,
		LongestSilence: &longest,
		Silences:       soakledger.Silences(from, moments),
	}
	if !reading.Covered.Empty {
		coverage.Oldest, coverage.Newest = reading.Covered.Oldest, reading.Covered.Newest
	}
	// Where the runtime dozed: nothing in those stretches is judged, so the
	// collection records them beside the silences it measured.
	dozing, err := DozingSpans(reading.Records)
	if err != nil {
		return soakledger.Coverage{}, err
	}
	coverage.Dozing = dozing
	return coverage, nil
}
