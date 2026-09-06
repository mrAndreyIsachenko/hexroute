package ingressagent

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/buildinfo"
	"github.com/mrAndreyIsachenko/hexroute/internal/objectstore"
)

const (
	outputSchema = "hexroute.ingress-agent.v1"
	runTimeout   = 60 * time.Second
)

// Run performs one synchronisation and exits.
//
// It is a command rather than a daemon because the host decides when to look:
// a timer on the host is an action the host takes, and there is nothing for a
// long-lived process to listen to.
func Run(
	ctx context.Context,
	args []string,
	lookup LookupEnv,
	stdout io.Writer,
	stderr io.Writer,
) int {
	if ctx == nil || lookup == nil || stdout == nil || stderr == nil {
		return 1
	}
	flags := flag.NewFlagSet("hexroute-ingress-agent", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	check := flags.Bool("check", false, "validate the environment and exit")
	version := flags.Bool("version", false, "print the build and exit")
	returnRetained := flags.Bool("return", false,
		"return to the retained version without reaching the network")
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		writeError(stderr, "usage")
		return 2
	}
	if *version {
		_, _ = fmt.Fprintf(stdout, "hexroute-ingress-agent version=%s commit=%s\n",
			buildinfo.Version, buildinfo.Commit)
		return 0
	}

	config, err := LoadConfig(lookup)
	if err != nil {
		writeError(stderr, "environment")
		return 1
	}
	pinned, err := ReadPinnedKey(config.PublicKeyFile)
	if err != nil {
		writeError(stderr, "public_key")
		return 1
	}
	if *check {
		return 0
	}

	store, err := NewStore(config.StateDir)
	if err != nil {
		writeError(stderr, "state")
		return 1
	}
	applier, err := NewFileApplier(config.ConfigPath, config.ReloadCommand, config.ReloadArgs)
	if err != nil {
		writeError(stderr, "applier")
		return 1
	}
	fetcher, err := objectstore.NewVersionStore(config.Store, objectstore.AccessFetch)
	if err != nil {
		writeError(stderr, "store")
		return 1
	}
	agent, err := New(fetcher, applier, store, pinned, config.Target)
	if err != nil {
		writeError(stderr, "agent")
		return 1
	}

	runContext, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()
	result, err := run(runContext, agent, *returnRetained)
	if err != nil {
		writeError(stderr, "failed")
		return 1
	}
	_ = json.NewEncoder(stdout).Encode(struct {
		Schema string `json:"schema"`
		Result
	}{Schema: outputSchema, Result: result})

	// A refusal is reported as a refusal. The host is still serving, so this
	// is not a crash, and it is not success either: something was published
	// that this host would not run, and a timer that reported nothing would
	// leave that invisible.
	if result.Outcome == OutcomeRefused {
		return 3
	}
	return 0
}

func run(ctx context.Context, agent *Agent, returnRetained bool) (Result, error) {
	if returnRetained {
		return agent.Return(ctx)
	}
	return agent.Sync(ctx)
}

func writeError(stderr io.Writer, code string) {
	_, _ = fmt.Fprintf(stderr, "{\"schema\":%q,\"error\":%q}\n", outputSchema, code)
}
