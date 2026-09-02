// Command hexroute-archive-annotate asks a local model to add notes to a
// report that is already finished.
//
// It is not part of the weekly review and nothing runs it on a schedule. A
// report is complete without it; this only ever adds a sentence to a finding
// the deterministic ranking already produced.
//
// The guarantee is checked at runtime and not only in tests: the report's
// digest is recomputed after the notes are attached and must not have moved.
// If it did, the model changed something it may not change, and the annotated
// report is refused rather than written.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/archivecommentary"
	"github.com/mrAndreyIsachenko/hexroute/internal/buildinfo"
	"github.com/mrAndreyIsachenko/hexroute/internal/eventarchive"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

// DefaultModel is what the calibration bench measured. Pointing this at an
// unmeasured model is a decision, so it has to be typed.
const DefaultModel = "qwen3.5:9b"

// DefaultTimeout bounds one answer. The bench watched this model take over
// half an hour on a bad day, and a weekly report that waits that long is a
// weekly report nobody runs twice.
const DefaultTimeout = 5 * time.Minute

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	return runWith(args, stdout, stderr, askOllama)
}

// asker returns the model's answer, or an error saying it could not.
type asker func(ctx context.Context, model, prompt string) (string, error)

// errModelUnavailable separates "there is no model here" from "the model
// answered badly". A report should say which.
var errModelUnavailable = errors.New("no local model is available")

func runWith(args []string, stdout, stderr io.Writer, ask asker) int {
	flags := flag.NewFlagSet("hexroute-archive-annotate", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	showVersion := flags.Bool("version", false, "print version")
	source := flags.String("report", "", "report to annotate")
	model := flags.String("model", DefaultModel, "local model to ask")
	timeout := flags.Duration("timeout", DefaultTimeout, "how long to wait")
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "error: invalid arguments")
		return 2
	}
	if *showVersion {
		fmt.Fprintf(stdout, "hexroute-archive-annotate version=%s commit=%s\n",
			buildinfo.Version, buildinfo.Commit)
		return 0
	}
	if *source == "" {
		fmt.Fprintln(stderr, "error: --report is required")
		return 2
	}
	if *timeout <= 0 {
		fmt.Fprintln(stderr, "error: --timeout must be positive")
		return 2
	}

	raw, err := os.ReadFile(*source)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	var report eventarchive.Report
	if err := json.Unmarshal(raw, &report); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	// A report that does not match its own digest is not one to annotate.
	// Adding notes to it would produce something that looks checked.
	expected := report.Digest
	recomputed, err := report.Digested()
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	if expected == "" || recomputed != expected {
		fmt.Fprintln(stderr,
			"error: the report does not match its own digest")
		return 2
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	annotated := report
	answer, askErr := ask(ctx, *model, archivecommentary.Prompt(report))
	switch {
	case errors.Is(askErr, errModelUnavailable):
		annotated = archivecommentary.Absent(
			report, *model, eventarchive.CommentaryUnavailable)
	case askErr != nil && ctx.Err() != nil:
		annotated = archivecommentary.Absent(
			report, *model, eventarchive.CommentaryTimedOut)
	case askErr != nil:
		annotated = archivecommentary.Absent(
			report, *model, eventarchive.CommentaryUnusable)
	default:
		annotated, _, _ = archivecommentary.Attach(report, *model, answer)
	}

	// The runtime form of the whole rule. Everything the ranking decided is
	// outside the digest's blind spot, so a digest that moved means the model
	// reached something it may not reach.
	after, err := annotated.Digested()
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	if after != expected {
		fmt.Fprintln(stderr,
			"error: annotating moved the report's digest; refusing to write it")
		return 1
	}

	_, encoded, err := policy.CanonicalSHA256(annotated)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	if err := publish(*source, encoded); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}

	pass := annotated.Pass
	fmt.Fprintf(stdout, "%s\n  %s: %s, %d attached, %d refused\n",
		*source, pass.Model, pass.Outcome, pass.Attached, pass.Rejected)
	return 0
}

// askOllama runs the model. It is the only thing here that executes anything.
func askOllama(ctx context.Context, model, prompt string) (string, error) {
	binary, err := exec.LookPath("ollama")
	if err != nil {
		return "", errModelUnavailable
	}
	command := exec.CommandContext(ctx, binary, "run", model)
	command.Stdin = newReader(prompt)
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	return string(output), nil
}

func newReader(value string) io.Reader { return &stringReader{value: value} }

type stringReader struct {
	value string
	at    int
}

func (reader *stringReader) Read(into []byte) (int, error) {
	if reader.at >= len(reader.value) {
		return 0, io.EOF
	}
	written := copy(into, reader.value[reader.at:])
	reader.at += written
	return written, nil
}

// publish writes with the discipline the archive uses, so an annotation
// interrupted halfway does not leave half a report where a whole one was.
func publish(path string, encoded []byte) error {
	staged := path + ".partial"
	file, err := os.OpenFile(staged, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	published := false
	defer func() {
		_ = file.Close()
		if !published {
			_ = os.Remove(staged)
		}
	}()
	if _, err := file.Write(append(encoded, '\n')); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(staged, path); err != nil {
		return err
	}
	published = true
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
