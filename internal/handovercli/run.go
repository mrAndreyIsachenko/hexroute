// Package handovercli is the operator's handover command.
//
// The transaction is held in the foreground by the person who ran it. Giving a
// daemon the authority to stop another runtime's job would outlive the forty
// seconds it is needed for, and a written procedure is not a transaction at all.
package handovercli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/buildinfo"
	"github.com/mrAndreyIsachenko/hexroute/internal/configversion"
	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
	"github.com/mrAndreyIsachenko/hexroute/internal/rootdaemon"
	"github.com/mrAndreyIsachenko/hexroute/internal/tunnelclaim"
	"github.com/mrAndreyIsachenko/hexroute/internal/tunnelhandover"
	"github.com/mrAndreyIsachenko/hexroute/internal/tunnelstart"
)

// Run is the command, taking its arguments and writers so a test can drive it.
//
// It lives here rather than in main because a command whose only entry point is
// main is a command no test invokes: this one shipped with two flags of the same
// name, which panics on the first run and was found on the machine.
func Run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("hexroute-handover", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	showVersion := flags.Bool("version", false, "print version")
	configPath := flags.String("config", "", "root observation configuration")
	claimPath := flags.String("claim", tunnelclaim.DefaultPath, "where ownership is recorded")
	sessionPath := flags.String("session", tunnelhandover.DefaultPath, "where the transaction records its phase")
	proofs := flags.Int("proofs", 2, "consecutive proofs of traversal that complete it")
	deadline := flags.Duration("deadline", 120*time.Second, "how long the whole attempt may take")
	between := flags.Duration("between", 5*time.Second, "wait between proofs")
	possession := flags.Duration("possession", 30*time.Second,
		"how long to wait for the previous holder of the tunnel to be gone")
	versionPath := flags.String("tunnel-version", "", "the signed tunnel configuration version")
	targetKey := flags.String("target-key", "", "what this host is, for the version's target")
	singBox := flags.String("sing-box", "", "the tunnel binary")
	contentPath := flags.String("content", "", "where the verified configuration is written")
	if flags.Parse(args) != nil {
		fmt.Fprintln(stderr, "usage: hexroute-handover [flags] begin|check|rehearse|abort")
		return 2
	}
	// Asked before a subcommand is required: --version is a question about the
	// binary, not about a handover, and demanding one to answer it made the
	// flag unusable.
	if *showVersion {
		fmt.Fprintf(stdout, "hexroute-handover version=%s commit=%s\n",
			buildinfo.Version, buildinfo.Commit)
		return 0
	}
	if flags.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: hexroute-handover [flags] begin|check|rehearse|abort")
		return 2
	}

	sessions, err := tunnelhandover.OpenStore(*sessionPath)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	claims, err := tunnelclaim.Open(*claimPath)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 1
	}
	transaction := &tunnelhandover.Transaction{
		Store: sessions, Claim: claims,
		Policy: tunnelhandover.Policy{
			Proofs: *proofs, Deadline: *deadline, Between: *between,
			Possession: *possession,
		},
	}

	switch flags.Arg(0) {
	case "begin":
		// What it needs to start a tunnel is checked before anything else,
		// because the alternative is reaching the claim with nothing to start:
		// the supervisor stops starting the tunnel and this runtime never
		// starts one.
		//
		// The starter is assigned only once it exists. A nil pointer placed in
		// the interface would not be nil, and the transaction's own refusal to
		// run without something to start would not fire.
		starter, err := tunnelStarter(
			*configPath, *versionPath, *targetKey, *singBox, *contentPath)
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 2
		}
		prover, err := payloadProver(*configPath)
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 2
		}
		incumbent, err := tunnelIncumbent()
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 2
		}
		transaction.Prover = prover
		transaction.Tunnel = starter
		transaction.Incumbent = incumbent
		outcome, err := transaction.Run(context.Background(), transactionID(), false)
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		return report(stdout, outcome)
	case "check":
		// Everything begin refuses on, and nothing begin does.
		//
		// A begin that refuses has already placed the claim, which tells the
		// previous owner to step back; the machine then has no tunnel until
		// somebody aborts. This asks the same questions through the same code
		// beforehand.
		return check(stdout, stderr, transaction,
			*configPath, *versionPath, *targetKey, *singBox, *contentPath)
	case "abort":
		outcome, err := transaction.Abort()
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		return report(stdout, outcome)
	case "rehearse":
		prover, err := payloadProver(*configPath)
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 2
		}
		transaction.Prover = prover
		outcome, err := transaction.Run(context.Background(), transactionID(), true)
		if err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		return report(stdout, outcome)
	default:
		fmt.Fprintln(stderr, "usage: hexroute-handover [flags] begin|check|rehearse|abort")
		return 2
	}
}

// tunnelStarter verifies the signed version at every start and runs the tunnel
// from what the signature covers.
//
// The key is the one this host already pins for policy — deliberately the same,
// because signing a configuration a host will run is the same authority as
// signing a policy generation, and a second key would be a second thing to keep
// safe for no gain.
func tunnelStarter(
	configPath, versionPath, targetKey, binary, contentPath string,
) (*tunnelstart.Starter, error) {
	if versionPath == "" || targetKey == "" || binary == "" || contentPath == "" {
		return nil, errors.New(
			"--tunnel-version, --target-key, --sing-box and --content are required to begin")
	}
	config, err := rootdaemon.LoadConfig(configPath)
	if err != nil {
		return nil, err
	}
	if config.PolicyControl == nil || len(config.PolicyControl.PinnedPublicKey) == 0 {
		return nil, errors.New("this host pins no operator key to verify a version against")
	}
	return &tunnelstart.Starter{
		ArtifactPath: versionPath,
		PublicKey:    config.PolicyControl.PinnedPublicKey,
		Target: configversion.Target{
			Kind: configversion.TargetNode, Key: targetKey,
		},
		ContentPath: contentPath,
		Binary:      binary,
		Runner:      tunnelstart.ExecRunner{},
	}, nil
}

// tunnelIncumbent is whoever holds the tunnel now.
//
// It is built from the observer the root daemon decides process_gone with, so
// the process this stops is by construction the process the decision record
// calls the tunnel.
func tunnelIncumbent() (*tunnelstart.Incumbent, error) {
	observer, err := observe.NewProcessObserver(observe.ExecRunner{})
	if err != nil {
		return nil, err
	}
	return &tunnelstart.Incumbent{
		Observer: observer,
		Runner:   tunnelstart.ExecRunner{},
	}, nil
}

// check reports every precondition, and does not stop at the first failure.
//
// Stopping would hand the operator one fault at a time, and each round of that
// is a run of the ceremony. What they want to know is whether the handover is
// ready, which is a statement about all of them.
func check(
	stdout, stderr io.Writer,
	transaction *tunnelhandover.Transaction,
	configPath, versionPath, targetKey, singBox, contentPath string,
) int {
	failures := 0
	say := func(name string, err error, detail string) {
		if err != nil {
			failures++
			fmt.Fprintf(stdout, "REFUSED  %-22s %v\n", name, err)
			return
		}
		fmt.Fprintf(stdout, "ok       %-22s %s\n", name, detail)
	}

	starter, err := tunnelStarter(configPath, versionPath, targetKey, singBox, contentPath)
	if err != nil {
		say("signed version", err, "")
	} else if content, err := starter.Verify(); err != nil {
		say("signed version", err, "")
	} else {
		say("signed version", nil, fmt.Sprintf("%d bytes verified for %s", len(content), targetKey))
	}

	if info, err := os.Stat(singBox); err != nil {
		say("tunnel binary", err, "")
	} else if info.Mode()&0o111 == 0 {
		say("tunnel binary", errors.New("not executable"), "")
	} else {
		say("tunnel binary", nil, singBox)
	}

	if prover, err := payloadProver(configPath); err != nil {
		say("payload probe", err, "")
	} else if traversed, err := prover.Traversed(context.Background()); err != nil {
		say("payload probe", err, "")
	} else if !traversed {
		say("payload probe", errors.New(
			"the payload does not traverse now; the handover would abort on its own evidence"), "")
	} else {
		say("payload probe", nil, "traffic traverses the tunnel today")
	}

	// Read through the same store the transaction uses, so "nothing in flight"
	// means what the transaction will mean by it.
	if session, inFlight, err := transaction.Store.Read(); err != nil {
		say("nothing in flight", err, "")
	} else if inFlight {
		say("nothing in flight", fmt.Errorf(
			"%s is abandoned at %s; abort it first", session.Transaction, session.Phase), "")
	} else {
		say("nothing in flight", nil, "no session left behind")
	}

	if claim, held, err := transaction.Claim.(claimReader).Held(); err != nil {
		say("tunnel unclaimed", err, "")
	} else if held {
		say("tunnel unclaimed", fmt.Errorf(
			"already claimed by %s", claim.Transaction), "")
	} else {
		say("tunnel unclaimed", nil, "the previous owner still holds it")
	}

	if incumbent, err := tunnelIncumbent(); err != nil {
		say("tunnel process", err, "")
	} else if pid, running, err := incumbent.Running(context.Background()); err != nil {
		say("tunnel process", err, "")
	} else if !running {
		// Not a refusal. Nothing to take is an ordinary state, and the
		// transaction proceeds; it is reported because an operator expecting to
		// take a running tunnel should know they are not.
		say("tunnel process", nil, "none running; nothing to take over")
	} else {
		say("tunnel process", nil, fmt.Sprintf("pid %d would be stopped first", pid))
	}

	fmt.Fprintln(stdout)
	if failures > 0 {
		fmt.Fprintf(stderr, "%d preconditions refused; begin would not complete\n", failures)
		return 1
	}
	fmt.Fprintln(stdout, "every precondition holds")
	return 0
}

// claimReader is the reading half of the claim. The transaction only ever
// writes it, so the interface it holds does not carry this.
type claimReader interface {
	Held() (tunnelclaim.Claim, bool, error)
}

func report(stdout io.Writer, outcome tunnelhandover.Outcome) int {
	encoded, err := json.Marshal(outcome)
	if err != nil {
		return 1
	}
	fmt.Fprintf(stdout, "%s\n", encoded)
	if outcome.Completed {
		return 0
	}
	return 1
}

func transactionID() string {
	return fmt.Sprintf("handover-%d", time.Now().UTC().Unix())
}

// payloadProver is the probe that answers whether traffic traversed, built from
// the same configuration the daemon uses so the two cannot disagree about what
// traversal means.
func payloadProver(configPath string) (tunnelhandover.Prover, error) {
	if configPath == "" {
		return nil, errors.New("--config is required to prove traversal")
	}
	config, err := rootdaemon.LoadConfig(configPath)
	if err != nil {
		return nil, err
	}
	if config.TunnelSupervision == nil {
		return nil, errors.New("this configuration has no payload probe to prove traversal with")
	}
	return &payloadProbe{
		prober:   observe.NewPayloadProber(),
		endpoint: config.TunnelSupervision.Payload,
	}, nil
}

type payloadProbe struct {
	prober   *observe.PayloadProber
	endpoint observe.PayloadEndpoint
}

func (probe *payloadProbe) Traversed(ctx context.Context) (bool, error) {
	observation, err := probe.prober.Payload(ctx, probe.endpoint)
	if err != nil {
		return false, nil
	}
	return observation.Traversed, nil
}
