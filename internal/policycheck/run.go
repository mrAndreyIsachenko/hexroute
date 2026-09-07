// Package policycheck answers whether a daemon would accept a prepared
// configuration, before that configuration is anywhere a daemon can reach.
//
// It exists because installing a policy control block into the root-owned
// configuration is the step in this system with the worst failure mode: an
// invalid block leaves the daemon refusing to start, launchd retrying it, and
// the diagnosis to be read from logs as root on a machine that no longer has a
// root observer. Nothing could check that block beforehand.
//
// It is a separate unprivileged command rather than a subcommand of the
// installer because the installer is forbidden from importing the daemons at
// all: it runs as root, and it must not carry their authority. This command
// carries no authority of its own — it opens a file and answers a question.
package policycheck

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/mrAndreyIsachenko/hexroute/internal/buildinfo"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/rootdaemon"
	"github.com/mrAndreyIsachenko/hexroute/internal/userdaemon"
)

const resultSchema = "hexroute.policy-config-check.v1"

var (
	errInvalidArguments = errors.New("invalid arguments")
	errInvalidConfig    = errors.New("configuration would be rejected")
)

type result struct {
	Schema           string        `json:"schema"`
	Command          string        `json:"command"`
	Domain           policy.Domain `json:"domain"`
	BundleGeneration uint64        `json:"bundle_generation"`
	PolicyGeneration uint64        `json:"policy_generation"`
	StaticSHA256     string        `json:"static_sha256"`
}

func Run(args []string, stdout, stderr io.Writer) int {
	if stdout == nil || stderr == nil {
		return 1
	}
	if len(args) == 1 && args[0] == "--check" {
		return 0
	}
	if len(args) == 1 && args[0] == "--version" {
		_, _ = fmt.Fprintf(stdout, "hexroute-policy-check version=%s commit=%s\n",
			buildinfo.Version, buildinfo.Commit)
		return 0
	}
	output, err := checkConfig(args)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "%s\n", err)
		if errors.Is(err, errInvalidArguments) {
			return 2
		}
		return 1
	}
	if json.NewEncoder(stdout).Encode(output) != nil {
		return 1
	}
	return 0
}

func checkConfig(args []string) (result, error) {
	flags := flag.NewFlagSet("hexroute-policy-check", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	domainValue := flags.String("domain", "", "root or user policy domain")
	configPath := flags.String("config", "", "prepared daemon configuration file")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *configPath == "" {
		return result{}, errInvalidArguments
	}
	domain := policy.Domain(*domainValue)
	if !domain.Valid() {
		return result{}, errInvalidArguments
	}
	installed, err := Installed(domain, *configPath)
	if err != nil {
		return result{}, err
	}
	return result{
		Schema: resultSchema, Command: "check-config", Domain: domain,
		BundleGeneration: installed.CurrentBundleGeneration,
		PolicyGeneration: installed.CurrentPolicyGeneration,
		StaticSHA256:     installed.StaticSHA256,
	}, nil
}

// Installed loads a prepared configuration through the daemon's own LoadConfig
// rather than restating its rules.
//
// A checker with its own copy of the rules drifts from the daemon, and a
// drifted checker accepts a file the daemon rejects — which is worse than no
// checker at all, because it supplies confidence immediately before an
// irreversible step.
//
// It deliberately does not check ownership, mode or path: the file is not
// installed yet, so it cannot have the identity the installed one must have.
// This answers whether the content is acceptable; installation answers the rest.
func Installed(domain policy.Domain, path string) (policy.InstalledCompatibility, error) {
	switch domain {
	case policy.DomainRoot:
		runtime, err := rootdaemon.LoadConfig(path)
		if err != nil || runtime.PolicyControl == nil {
			return policy.InstalledCompatibility{}, errInvalidConfig
		}
		return runtime.PolicyControl.Installed, nil
	case policy.DomainUser:
		runtime, err := userdaemon.LoadConfig(path)
		if err != nil || runtime.PolicyControl == nil {
			return policy.InstalledCompatibility{}, errInvalidConfig
		}
		return runtime.PolicyControl.Installed, nil
	default:
		return policy.InstalledCompatibility{}, errInvalidArguments
	}
}
