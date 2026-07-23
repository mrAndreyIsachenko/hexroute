package command

import (
	"flag"
	"fmt"
	"io"

	"github.com/mrAndreyIsachenko/hexroute/internal/buildinfo"
	"github.com/mrAndreyIsachenko/hexroute/internal/logging"
)

func Run(name string, args []string, stdout, stderr io.Writer) int {
	component, err := logging.ParseComponent(name)
	if err != nil {
		return 2
	}
	infoLog, err := logging.New(stdout, component)
	if err != nil {
		return 1
	}
	errorLog, err := logging.New(stderr, component)
	if err != nil {
		return 1
	}

	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	showVersion := flags.Bool("version", false, "print version")
	check := flags.Bool("check", false, "run a non-mutating startup check")

	if err := flags.Parse(args); err != nil {
		_ = errorLog.Emit(
			logging.LevelWarn,
			logging.EventArgumentRejected,
			logging.ResultRejected,
			logging.ReasonInvalidFlags,
		)
		return 2
	}
	if flags.NArg() != 0 {
		_ = errorLog.Emit(
			logging.LevelWarn,
			logging.EventArgumentRejected,
			logging.ResultRejected,
			logging.ReasonUnexpectedArguments,
		)
		return 2
	}

	if *showVersion {
		if err := infoLog.Emit(
			logging.LevelInfo,
			logging.EventVersionRequested,
			logging.ResultReported,
			"",
		); err != nil {
			return 1
		}
		fmt.Fprintf(stdout, "%s version=%s commit=%s\n", name, buildinfo.Version, buildinfo.Commit)
		return 0
	}
	if *check {
		if err := infoLog.Emit(logging.LevelInfo, logging.EventStartupCheck, logging.ResultOK, ""); err != nil {
			return 1
		}
		return 0
	}

	if err := infoLog.Emit(
		logging.LevelInfo,
		logging.EventCommandStatus,
		logging.ResultSkeleton,
		"",
	); err != nil {
		return 1
	}
	return 0
}
