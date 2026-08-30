// Command hexroute-connectivity-qualify injects the canonical fault traces and
// records what each one made visible.
//
// It is the producer the qualification chain was missing. Before it the
// thirteen faults were thirteen paragraphs with digests, and the gate rested
// on the claim that the read model would have behaved as they described.
//
// Everything runs under a scratch root this command creates. It never opens
// the daemon's store: several of these faults are deliberate corruption of a
// checkpoint lineage, and corrupting the running host's lineage to prove it
// refuses corruption would break the host in exactly the way the fault
// describes. Nothing here opens a socket, starts a daemon or touches a route.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/buildinfo"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityqualification"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivitysoak"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivitytrace"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const outputSchema = "hexroute.connectivity-qualify.v1"

type report struct {
	Schema   string                              `json:"schema"`
	Session  string                              `json:"session"`
	Results  []connectivitysoak.Outcome          `json:"results"`
	Chain    string                              `json:"chain,omitempty"`
	Recorded int                                 `json:"recorded"`
	Progress *connectivityqualification.Progress `json:"qualification,omitempty"`
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("hexroute-connectivity-qualify", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	showVersion := flags.Bool("version", false, "print version")
	scratch := flags.String("scratch", "", "empty directory the traces run under")
	chain := flags.String("qualification", "", "qualification chain root to append to")
	session := flags.String("session", "", "qualification session identity")
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "error: invalid arguments")
		return 2
	}
	if *showVersion {
		fmt.Fprintf(stdout, "hexroute-connectivity-qualify version=%s commit=%s\n",
			buildinfo.Version, buildinfo.Commit)
		return 0
	}
	if *scratch == "" {
		fmt.Fprintln(stderr, "error: --scratch is required")
		return 2
	}
	if _, err := metadata.ParseUUID(*session); err != nil {
		// Without a session there is nothing to keep two runs apart, and a
		// chain holding two runs adds up to a number about neither.
		fmt.Fprintln(stderr, "error: --session must be a UUID")
		return 2
	}

	outcomes, err := inject(*scratch)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	out := report{Schema: outputSchema, Session: *session, Results: outcomes}

	if *chain != "" {
		out.Chain = *chain
		recorded, progress, recordErr := record(*chain, *session, outcomes)
		if recordErr != nil {
			fmt.Fprintf(stderr, "error: %v\n", recordErr)
			return 1
		}
		out.Recorded = recorded
		out.Progress = &progress
	}
	if err := json.NewEncoder(stdout).Encode(out); err != nil {
		return 1
	}

	// A trace that did not produce what it said it would is the finding this
	// command exists to surface, so it leaves a failing status behind.
	for _, outcome := range outcomes {
		if outcome.GuessedHealthy {
			fmt.Fprintf(stderr, "error: %s left the read model looking untroubled\n",
				outcome.Fault)
			return 1
		}
	}
	for _, outcome := range outcomes {
		if !outcome.Matched {
			fmt.Fprintf(stderr, "error: %s: %s\n", outcome.Fault, outcome.Mismatch)
			return 1
		}
	}
	return 0
}

// inject runs every canonical trace, each in its own directory.
func inject(scratch string) ([]connectivitysoak.Outcome, error) {
	absolute, err := filepath.Abs(scratch)
	if err != nil {
		return nil, fmt.Errorf("scratch: %w", err)
	}
	traces, err := connectivitytrace.Canonical()
	if err != nil {
		return nil, fmt.Errorf("canonical traces: %w", err)
	}
	outcomes := make([]connectivitysoak.Outcome, 0, len(traces))
	for _, trace := range traces {
		outcome, runErr := connectivitysoak.Run(trace,
			filepath.Join(absolute, string(trace.Fault)))
		if runErr != nil {
			return nil, fmt.Errorf("%s: %w", trace.Fault, runErr)
		}
		outcomes = append(outcomes, outcome)
	}
	return outcomes, nil
}

// record appends one evidence record per trace and reports what the chain now
// adds up to.
//
// Each record binds to the evidence its own trace produced. A run that stamped
// them all with one binding would be recording thirteen results against one
// piece of evidence, which is a chain that verifies and means nothing.
func record(
	root, session string,
	outcomes []connectivitysoak.Outcome,
) (int, connectivityqualification.Progress, error) {
	if len(outcomes) == 0 {
		return 0, connectivityqualification.Progress{}, fmt.Errorf("nothing to record")
	}
	opening := bindingOf(metadata.UUID(session), outcomes[0])
	recorder, err := connectivityqualification.OpenRecorder(root, opening)
	if err != nil {
		return 0, connectivityqualification.Progress{}, fmt.Errorf("chain: %w", err)
	}
	written := 0
	for _, outcome := range outcomes {
		binding := bindingOf(metadata.UUID(session), outcome)
		result := connectivityqualification.ResultExpected
		if !outcome.Matched {
			result = connectivityqualification.ResultDiverged
		}
		observed := outcome
		_, appendErr := recorder.Append(
			connectivityqualification.KindFaultInjection, result,
			time.Now().UTC().Format(time.RFC3339Nano), 0,
			func(record *connectivityqualification.EvidenceRecord) {
				record.Binding = binding
				record.FaultInjection = &connectivityqualification.FaultInjection{
					Fault:       observed.Fault,
					TraceSHA256: observed.TraceSHA256,
					// What the model actually reported, not what the trace
					// hoped for: a mismatch is recorded as what was seen.
					Visible:        visibleOf(observed),
					GuessedHealthy: observed.GuessedHealthy,
				}
			})
		if appendErr != nil {
			return written, connectivityqualification.Progress{},
				fmt.Errorf("%s: %w", outcome.Fault, appendErr)
		}
		written++
	}
	progress, err := connectivityqualification.Inspect(root, opening)
	if err != nil {
		return written, connectivityqualification.Progress{}, err
	}
	return written, progress, nil
}

func visibleOf(outcome connectivitysoak.Outcome) string {
	if outcome.Matched {
		return outcome.Visible
	}
	return outcome.Mismatch
}

func bindingOf(
	session metadata.UUID,
	outcome connectivitysoak.Outcome,
) connectivityqualification.Binding {
	return connectivityqualification.Binding{
		SessionID:       session,
		BootID:          outcome.Observation.BootID,
		CheckpointID:    outcome.Observation.CheckpointID,
		SnapshotSHA256:  outcome.Observation.SnapshotSHA256,
		DiffSHA256:      outcome.Observation.DiffSHA256,
		ProposalsSHA256: outcome.Observation.ProposalsSHA256,
	}
}
