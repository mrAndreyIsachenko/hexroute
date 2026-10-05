package observe

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// A line longer than an observation keeps is skipped, and the lines around it
// are still read.
//
// Somebody else's arguments are not this observation's problem: measured
// 2026-09-14, two unrelated processes with command lines of 5,758 and 6,109
// bytes refused a whole listing, and on 2026-10-05 the listing's total size did
// the same. Neither may cost the runtime its sight of the tunnel.
func TestAnOverlongLineIsSkippedAndTheRestIsRead(t *testing.T) {
	script := fmt.Sprintf(
		`printf 'first\n%s\nlast\n'`, strings.Repeat("x", MaxObservationLine+1000))
	kept, err := ExecRunner{}.OutputLines(
		context.Background(),
		func([]byte) bool { return true },
		"/bin/sh", "-c", script)
	if err != nil {
		t.Fatalf("OutputLines: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(kept), "\n"), "\n")
	if len(lines) != 2 || lines[0] != "first" || lines[1] != "last" {
		t.Fatalf("kept %d lines: %q", len(lines), lines)
	}
}

// And what the keeper refuses is not kept.
func TestOnlyTheLinesTheKeeperWantsAreKept(t *testing.T) {
	kept, err := ExecRunner{}.OutputLines(
		context.Background(),
		func(line []byte) bool { return strings.Contains(string(line), "wanted") },
		"/bin/sh", "-c", `printf 'wanted one\nnot this\nwanted two\n'`)
	if err != nil {
		t.Fatalf("OutputLines: %v", err)
	}
	if string(kept) != "wanted one\nwanted two\n" {
		t.Fatalf("kept %q", kept)
	}
}

// A command that fails is a failure, whatever it printed first.
func TestACommandThatFailsIsStillAFailure(t *testing.T) {
	if _, err := (ExecRunner{}).OutputLines(
		context.Background(),
		func([]byte) bool { return true },
		"/bin/sh", "-c", `printf 'something\n'; exit 3`); err == nil {
		t.Fatal("a command that exited 3 was read as a success")
	}
}

// Lines the keeper wanted and the limit could not hold are a refusal, not a
// shorter answer.
//
// A listing with lines missing can say something is absent when it is there,
// and the two answers this observation gives — running and absent — are what a
// cause is decided from. Keeping only the lines that could matter is what puts
// this out of reach; reaching it anyway is not an answer.
func TestKeptLinesPastTheLimitAreRefused(t *testing.T) {
	script := `printf 'wanted %s\nwanted %s\n'`
	filler := strings.Repeat("y", 400)
	_, err := ExecRunner{MaxOutput: 500}.OutputLines(
		context.Background(),
		func([]byte) bool { return true },
		"/bin/sh", "-c", fmt.Sprintf(script, filler, filler))
	if !errors.Is(err, ErrOutputTooLarge) {
		t.Fatalf("OutputLines = %v, want %v", err, ErrOutputTooLarge)
	}
}
