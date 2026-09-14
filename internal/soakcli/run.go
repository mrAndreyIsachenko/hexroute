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
		if !held {
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
		entries, err := RebuildEntries(reading.Records)
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 2
		}
		coverage := soakledger.Coverage{Requested: from, Records: reading.Covered.Records, CollectedAt: until}
		if !reading.Covered.Empty {
			coverage.Oldest, coverage.Newest = reading.Covered.Oldest, reading.Covered.Newest
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
	if err := soakledger.Continuous(windows, from, until); err != nil {
		fmt.Fprintf(stdout, "NOT JUDGEABLE  %v\n", err)
		return 1
	}
	entries, err := ledger.Decisions()
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	decisions := make([]soakcompare.Decision, 0, len(entries))
	for _, entry := range entries {
		decisions = append(decisions, soakcompare.Decision{At: entry.At, Causes: entry.Causes})
	}
	rebuilds, err := TwilightRebuilds(twilightPath)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	inductions, err := ledger.Inductions()
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	report, err := soakcompare.Compare(decisions, rebuilds, inductions, Window, from, until)
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
	}
	passed, missing := report.Passes(soakcompare.Pass)
	if !passed {
		fmt.Fprintf(stdout, "NOT PASSED  %s\n", strings.Join(missing, "; "))
		return 1
	}
	fmt.Fprintln(stdout, "PASSED")
	return 0
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
		entries = append(entries, soakledger.Entry{
			Sequence: record.Sequence, At: record.Metadata.WallClock.UTC(), Causes: decision.Causes,
		})
	}
	return entries, nil
}

// TwilightRebuilds is the owning runtime's rebuilds of the three judged kinds,
// read from its event log. That log is not evicted, so it is read whole at
// judgement rather than collected.
func TwilightRebuilds(path string) ([]soakcompare.Rebuild, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var rebuilds []soakcompare.Rebuild
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lines := 0
	for scanner.Scan() {
		lines++
		var transition struct {
			Timestamp string `json:"timestamp"`
			From      string `json:"from"`
			Event     string `json:"event"`
			Reason    string `json:"reason"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &transition); err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, lines, err)
		}
		if transition.Event != "" || transition.From == "" {
			continue
		}
		cause, ok := twilightReasons[transition.Reason]
		if !ok {
			continue
		}
		at, err := time.Parse(time.RFC3339, transition.Timestamp)
		if err != nil {
			return nil, fmt.Errorf("%s line %d: %w", path, lines, err)
		}
		rebuilds = append(rebuilds, soakcompare.Rebuild{At: at.UTC(), Cause: cause})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if lines == 0 {
		return nil, errors.New("the owning runtime's event log is empty; a judgement against it would be about nothing")
	}
	sort.Slice(rebuilds, func(i, j int) bool { return rebuilds[i].At.Before(rebuilds[j].At) })
	return rebuilds, nil
}
