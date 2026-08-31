// Command hexroute-connectivity-watch reports what changed about the read
// model since anyone last looked.
//
// It prints transitions and is silent when there are none, which is the only
// way a thing run every few minutes stays worth reading. It reads the store
// rather than the daemon, because a daemon that will not start is exactly the
// condition worth being told about, and it opens no socket, starts nothing and
// writes only its own memory of the last look.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mrAndreyIsachenko/hexroute/internal/buildinfo"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivitycheckpoint"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityqualification"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivitywatch"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("hexroute-connectivity-watch", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	showVersion := flags.Bool("version", false, "print version")
	root := flags.String("store", "", "connectivity read-model store root")
	chain := flags.String("qualification", "", "qualification chain root to watch")
	session := flags.String("session", "", "qualification session identity")
	statePath := flags.String("state", "", "where to remember the last look")
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "error: invalid arguments")
		return 2
	}
	if *showVersion {
		fmt.Fprintf(stdout, "hexroute-connectivity-watch version=%s commit=%s\n",
			buildinfo.Version, buildinfo.Commit)
		return 0
	}
	if *root == "" || *statePath == "" {
		fmt.Fprintln(stderr, "error: --store and --state are required")
		return 2
	}
	if (*chain == "") != (*session == "") {
		fmt.Fprintln(stderr, "error: --qualification and --session go together")
		return 2
	}

	previous, first, err := connectivitywatch.Load(*statePath)
	if err != nil {
		// A memory that cannot be read is not a first run. Treating it as one
		// would report nothing and call this a quiet host.
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	current := look(*root, *chain, *session)
	moves := connectivitywatch.Compare(previous, current, first)
	if err := connectivitywatch.Save(*statePath, current); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}

	for _, move := range moves {
		mark := " "
		if move.Regression {
			mark = "!"
		}
		fmt.Fprintf(stdout, "%s %-18s %s -> %s\n", mark, move.What, move.From, move.To)
	}
	if connectivitywatch.Regressed(moves) {
		// Anything scheduling this reads the status, not the lines.
		return 1
	}
	return 0
}

// look establishes what is true now. Every failure to read something is
// recorded as a fact rather than returned as an error, because "the store
// stopped being readable" is the finding, not an obstacle to reporting one.
func look(root, chain, session string) connectivitywatch.Facts {
	facts := connectivitywatch.Facts{}
	store, err := connectivitycheckpoint.Open(
		filepath.Join(root, "readmodel"), connectivitycheckpoint.Options{})
	if err != nil {
		facts.Failure = "store unavailable"
		return facts
	}
	resume, err := store.Resume()
	if err != nil {
		facts.Failure = "lineage unreadable"
		return facts
	}
	facts.Readable = true
	facts.Resume = string(resume.Status)
	facts.ResumeReason = string(resume.Reason)
	if resume.Checkpoint != nil {
		checkpoint := *resume.Checkpoint
		facts.LineageBroke = checkpoint.Break != nil
		summary := checkpoint.Snapshot.Summary
		facts.Aggregate = string(summary.State)
		facts.Authorization = string(summary.Authorization)
		facts.OpenGaps = summary.OpenGaps
		facts.SourceConflicts = summary.SourceConflicts
		facts.StaleComponents = summary.Stale
	}
	if chain != "" {
		facts.Qualification = lookAtChain(chain, session)
	}
	return facts
}

func lookAtChain(chain, session string) *connectivitywatch.QualificationFacts {
	records, err := connectivityqualification.ReadRecords(chain)
	if err != nil || len(records) == 0 {
		return &connectivitywatch.QualificationFacts{Blocking: "chain unreadable"}
	}
	binding := records[0].Binding
	binding.SessionID = metadata.UUID(session)
	progress, err := connectivityqualification.Inspect(chain, binding)
	if err != nil {
		return &connectivitywatch.QualificationFacts{Blocking: "chain does not replay"}
	}
	gate := connectivityqualification.GateFor(chain, binding)
	return &connectivitywatch.QualificationFacts{
		Diverged:        progress.Diverged,
		Unbound:         progress.Unbound,
		GuessedHealthy:  progress.GuessedHealthy,
		GatePassing:     gate.Passing(),
		Blocking:        progress.Blocking,
		EligibleSeconds: progress.EligibleSeconds,
	}
}
