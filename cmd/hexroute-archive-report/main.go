// Command hexroute-archive-report writes a dated summary of a window of the
// local event archive, and does nothing else.
//
// It opens no socket, starts nothing, reaches no network and changes nothing
// about the archive it reads. A review that could alter what it reviews is not
// a review, and the one time this runs is the week after something went wrong.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/buildinfo"
	"github.com/mrAndreyIsachenko/hexroute/internal/eventarchive"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

// DefaultWindow is the period a weekly review asks about.
const DefaultWindow = 7 * 24 * time.Hour

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	return runAt(args, stdout, stderr, time.Now)
}

// runAt takes its clock so a test can fix the date without the command
// carrying a flag that exists only for tests.
func runAt(args []string, stdout, stderr io.Writer, now func() time.Time) int {
	flags := flag.NewFlagSet("hexroute-archive-report", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	showVersion := flags.Bool("version", false, "print version")
	root := flags.String("archive", "", "local event archive root")
	out := flags.String("out", "", "directory to write the dated report into")
	window := flags.Duration("window", DefaultWindow, "how far back to review")
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "error: invalid arguments")
		return 2
	}
	if *showVersion {
		fmt.Fprintf(stdout, "hexroute-archive-report version=%s commit=%s\n",
			buildinfo.Version, buildinfo.Commit)
		return 0
	}
	if *root == "" || *out == "" {
		fmt.Fprintln(stderr, "error: --archive and --out are required")
		return 2
	}
	if *window <= 0 {
		fmt.Fprintln(stderr, "error: --window must be positive")
		return 2
	}

	archive, err := eventarchive.OpenForReading(*root)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	until := now().UTC()
	reading, err := archive.Read(eventarchive.Query{
		From: until.Add(-*window), To: until,
	})
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	report, err := eventarchive.Summarize(reading)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}

	// The canonical encoding is what the digest was taken over, so the file on
	// disk is the bytes the digest names rather than a second rendering of the
	// same values.
	_, encoded, err := policy.CanonicalSHA256(report)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	if err := os.MkdirAll(*out, 0o700); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}
	name := fmt.Sprintf("%s-archive-report.json", until.Format("2006-01-02"))
	path := filepath.Join(*out, name)
	if err := writeReport(path, encoded); err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}

	// An empty review says so on the way out. A report file that exists and a
	// line that mentions no records are easy to read as a quiet week, and the
	// difference between quiet and unanswerable is the whole point of the
	// covered window.
	coverage := "covering the requested window"
	if report.Shortened {
		coverage = "covering less than the requested window"
	}
	if report.Covered.Empty {
		coverage = "the archive held nothing in this window"
	}
	fmt.Fprintf(stdout, "%s\n  %d records, %s\n  digest %s\n",
		path, report.Records, coverage, report.Digest)
	return 0
}

// writeReport publishes the report with the same discipline the archive uses:
// staged, synchronised, renamed. A review interrupted halfway must not leave a
// file that looks like a report and is half of one.
func writeReport(path string, encoded []byte) error {
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
