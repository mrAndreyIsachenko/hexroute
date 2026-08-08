package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/buildinfo"
	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/policyqualification"
	"github.com/mrAndreyIsachenko/hexroute/internal/qualificationagent"
)

const defaultInterval = 60 * time.Second

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && args[0] == "--version" {
		fmt.Fprintf(stdout, "hexroute-policy-qualification version=%s commit=%s\n", buildinfo.Version, buildinfo.Commit)
		return 0
	}
	if len(args) == 1 && args[0] == "--check" {
		return 0
	}
	if len(args) == 0 {
		writeError(stderr, "invalid_command")
		return 2
	}
	command := args[0]
	options, ok := parseOptions(command, args[1:])
	if !ok {
		writeError(stderr, "invalid_command")
		return 2
	}
	agent, err := buildAgent(options)
	if err != nil {
		writeError(stderr, "invalid_configuration")
		return 1
	}
	ctx := context.Background()
	switch command {
	case "serve":
		ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
		defer cancel()
		if err := agent.Serve(ctx); err != nil {
			writeError(stderr, classify(err))
			return 1
		}
		return 0
	case "start":
		err = agent.Start(ctx)
	case "restart-session":
		err = agent.RestartSession(ctx)
	case "arm-sleep":
		err = agent.ArmSleep(ctx)
	case "import-fault":
		err = agent.ImportFault(ctx, policyqualification.Kind(options.kind), options.report)
	case "status":
		var status qualificationagent.Status
		status, err = agent.Status()
		if err == nil {
			err = json.NewEncoder(stdout).Encode(status)
		}
	default:
		writeError(stderr, "invalid_command")
		return 2
	}
	if err != nil {
		writeError(stderr, classify(err))
		return 1
	}
	if command != "status" && command != "serve" {
		_, _ = fmt.Fprintf(stdout, "ok: %s\n", command)
	}
	return 0
}

type options struct {
	root       string
	rootSocket string
	userSocket string
	interval   time.Duration
	maximumGap time.Duration
	kind       string
	report     string
}

func parseOptions(command string, args []string) (options, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return options{}, false
	}
	userSocket, err := ipc.UserSocketPath(home)
	if err != nil {
		return options{}, false
	}
	value := options{
		root:       filepath.Join(home, "Library/Application Support/Hexroute/policy-qualification"),
		rootSocket: ipc.RootSocketPath, userSocket: userSocket,
		interval: defaultInterval, maximumGap: 3 * defaultInterval,
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&value.root, "root", value.root, "private qualification root")
	flags.StringVar(&value.rootSocket, "root-socket", value.rootSocket, "root observe socket")
	flags.StringVar(&value.userSocket, "user-socket", value.userSocket, "user observe socket")
	flags.DurationVar(&value.interval, "interval", value.interval, "sample interval")
	flags.DurationVar(&value.maximumGap, "max-gap", value.maximumGap, "maximum unarmed sample gap")
	if command == "import-fault" {
		flags.StringVar(&value.kind, "kind", "", "fault kind")
		flags.StringVar(&value.report, "report", "", "bounded test report")
	}
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		return options{}, false
	}
	if command == "import-fault" && (value.kind == "" || !filepath.IsAbs(value.report)) {
		return options{}, false
	}
	switch command {
	case "serve", "start", "restart-session", "arm-sleep", "import-fault", "status":
		return value, true
	default:
		return options{}, false
	}
}

func buildAgent(options options) (*qualificationagent.Agent, error) {
	platform, err := qualificationagent.NewSystemPlatform()
	if err != nil {
		return nil, err
	}
	config := qualificationagent.Config{
		Root: options.root, RootSocket: options.rootSocket, UserSocket: options.userSocket,
		SampleInterval: options.interval, MaximumGap: options.maximumGap,
		ReadTimeout: 5 * time.Second,
	}
	reader := qualificationagent.LocalStatusReader{
		RootSocket: options.rootSocket, UserSocket: options.userSocket,
		Timeout: config.ReadTimeout,
	}
	return qualificationagent.New(config, reader, platform)
}

func classify(err error) string {
	switch {
	case errors.Is(err, qualificationagent.ErrStatusUnavailable):
		return "status_unavailable"
	case errors.Is(err, qualificationagent.ErrSessionInvalid):
		return "session_invalid"
	case errors.Is(err, qualificationagent.ErrUnsupportedPlatform):
		return "platform_unavailable"
	default:
		return "operation_failed"
	}
}

func writeError(writer io.Writer, reason string) {
	_, _ = fmt.Fprintf(writer, "error: policy qualification %s\n", reason)
}
