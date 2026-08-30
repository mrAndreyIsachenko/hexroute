// Command hexroute-connectivity-replay verifies a stored read-model lineage
// offline and lists the fault traces a qualification must inject.
//
// It reads. It opens no socket, starts no daemon, touches no route and never
// moves the policy active pointer. Pointed at a store after a soak it answers
// one question: do the retained facts still produce the conclusions that were
// published, link by link.
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
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivitycheckpoint"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityjournal"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivitytrace"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

const outputSchema = "hexroute.connectivity-replay.v1"

type report struct {
	Schema string `json:"schema"`
	Store  string `json:"store,omitempty"`

	Verify *connectivitycheckpoint.VerifyResult `json:"verify,omitempty"`
	Traces []traceReport                        `json:"traces,omitempty"`
}

type traceReport struct {
	Fault  connectivitytrace.Fault `json:"fault"`
	Layer  connectivitytrace.Layer `json:"layer"`
	Digest string                  `json:"digest"`
	// Visible is what the trace must make the read model report. It is stated
	// here so a qualification run is compared against it rather than against
	// whatever the run produced.
	Visible string `json:"visible"`
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("hexroute-connectivity-replay", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	showVersion := flags.Bool("version", false, "print version")
	root := flags.String("store", "", "connectivity read-model store root")
	listTraces := flags.Bool("traces", false, "list the canonical fault traces")
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "error: invalid arguments")
		return 2
	}
	if *showVersion {
		fmt.Fprintf(stdout, "hexroute-connectivity-replay version=%s commit=%s\n",
			buildinfo.Version, buildinfo.Commit)
		return 0
	}
	if *root == "" && !*listTraces {
		fmt.Fprintln(stderr, "error: --store or --traces is required")
		return 2
	}

	out := report{Schema: outputSchema}
	if *listTraces {
		traces, err := connectivitytrace.Canonical()
		if err != nil {
			fmt.Fprintln(stderr, "error: canonical traces unavailable")
			return 1
		}
		for _, trace := range traces {
			digest, digestErr := trace.Digest()
			if digestErr != nil {
				fmt.Fprintln(stderr, "error: trace digest unavailable")
				return 1
			}
			out.Traces = append(out.Traces, traceReport{
				Fault: trace.Fault, Layer: trace.Layer, Digest: digest,
				Visible: trace.Expectation.Visible,
			})
		}
	}

	unsound := false
	if *root != "" {
		out.Store = *root
		result, err := verify(*root)
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		out.Verify = &result
		unsound = !result.Sound()
	}
	if err := json.NewEncoder(stdout).Encode(out); err != nil {
		return 1
	}
	if unsound {
		// A lineage that contradicts its own evidence is the finding this
		// command exists to surface, so it leaves a failing status behind for
		// whatever ran it.
		fmt.Fprintln(stderr, "error: lineage diverged from its retained evidence")
		return 1
	}
	return 0
}

func verify(root string) (connectivitycheckpoint.VerifyResult, error) {
	store, err := connectivitycheckpoint.Open(
		filepath.Join(root, "readmodel"), connectivitycheckpoint.Options{})
	if err != nil {
		return connectivitycheckpoint.VerifyResult{}, fmt.Errorf("store: %w", err)
	}
	rootJournal, err := openJournal(filepath.Join(root, "root"), policy.DomainRoot)
	if err != nil {
		return connectivitycheckpoint.VerifyResult{}, err
	}
	userJournal, err := openJournal(filepath.Join(root, "user"), policy.DomainUser)
	if err != nil {
		return connectivitycheckpoint.VerifyResult{}, err
	}
	return connectivitycheckpoint.Verify(store, rootJournal, userJournal, nil)
}

// readOnlyClock exists because opening a journal wants one and this command
// writes nothing. A clock that is never read cannot make a record.
type readOnlyClock struct{}

func (readOnlyClock) WallNow() time.Time          { return time.Unix(0, 0).UTC() }
func (readOnlyClock) MonotonicNow() time.Duration { return 0 }

func openJournal(
	path string,
	domain policy.Domain,
) (*connectivityjournal.Journal, error) {
	journal, err := connectivityjournal.Open(path, domain,
		connectivityjournal.Options{
			NodeID: metadata.UUID("00000000-0000-4000-8000-000000000000"),
			Clock:  readOnlyClock{},
		})
	if err != nil {
		return nil, fmt.Errorf("%s journal: %w", domain, err)
	}
	return journal, nil
}
