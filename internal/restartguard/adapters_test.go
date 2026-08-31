package restartguard

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/connectivity"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityhost"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityjournal"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivityqualification"
	"github.com/mrAndreyIsachenko/hexroute/internal/connectivitywatch"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/safety"
)

const guardNodeID = metadata.UUID("22222222-2222-4222-8222-222222222222")

// openJournal starts a process over the connectivity journal.
//
// The next host sequence is derived from what the root already holds, which is
// the whole question: a journal that reopened blind would hand out a sequence
// the previous process already used, and the accepted order would have two
// different facts at one position.
func openJournal(tb testing.TB, root string) session {
	tb.Helper()
	journal, err := connectivityjournal.Open(
		filepath.Join(root, "root"), policy.DomainRoot,
		connectivityjournal.Options{NodeID: guardNodeID, Clock: metadata.NewSystemClock()})
	if err != nil {
		tb.Fatalf("open journal: %v", err)
	}
	watermark := func(tb testing.TB) uint64 {
		tb.Helper()
		records, err := journal.Records()
		if err != nil {
			tb.Fatalf("journal records: %v", err)
		}
		var highest uint64
		for _, record := range records {
			if record.HostSequence > highest {
				highest = record.HostSequence
			}
		}
		return highest
	}
	return session{
		write: func(tb testing.TB) string {
			tb.Helper()
			next := watermark(tb) + 1
			fact := connectivity.FixtureBaseline(connectivity.ComponentRelays, next)
			if err := journal.Append(
				fact, next, next, "accepted", safety.RoleAuthoritative); err != nil {
				tb.Fatalf("journal append: %v", err)
			}
			return strconv.FormatUint(next, 10)
		},
		seen: func(tb testing.TB) []string {
			tb.Helper()
			records, err := journal.Records()
			if err != nil {
				tb.Fatalf("journal records: %v", err)
			}
			held := make([]string, 0, len(records))
			for _, record := range records {
				held = append(held, strconv.FormatUint(record.HostSequence, 10))
			}
			return held
		},
		position: watermark,
	}
}

// openComparisonRecorder starts a process over the shadow comparison log.
func openComparisonRecorder(tb testing.TB, root string) session {
	tb.Helper()
	recorder, err := connectivityhost.OpenRecorder(root)
	if err != nil {
		tb.Fatalf("open recorder: %v", err)
	}
	logged := func(tb testing.TB) []string {
		tb.Helper()
		file, err := os.Open(filepath.Join(root, "shadow-comparisons.jsonl"))
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			tb.Fatalf("read comparisons: %v", err)
		}
		defer file.Close()
		var held []string
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			var comparison connectivityhost.Comparison
			if err := json.Unmarshal(scanner.Bytes(), &comparison); err != nil {
				tb.Fatalf("decode comparison: %v", err)
			}
			held = append(held, strconv.FormatUint(uint64(comparison.Divergent), 10))
		}
		if err := scanner.Err(); err != nil {
			tb.Fatalf("scan comparisons: %v", err)
		}
		return held
	}
	return session{
		write: func(tb testing.TB) string {
			tb.Helper()
			divergent := uint16(len(logged(tb)) + 1)
			wrote, err := recorder.Record(connectivityhost.Comparison{
				Schema:  connectivityhost.ComparisonSchema,
				Version: connectivityhost.ComparisonSchemaVersion,
				BootID:  connectivity.FixtureBootID, Divergent: divergent,
			})
			if err != nil {
				tb.Fatalf("record comparison: %v", err)
			}
			if !wrote {
				tb.Fatal("a comparison that differs from the last one was not written")
			}
			return strconv.FormatUint(uint64(divergent), 10)
		},
		seen:     logged,
		position: func(tb testing.TB) uint64 { return recorder.Written() },
	}
}

// openEvidenceChain starts a process over the qualification evidence chain.
func openEvidenceChain(tb testing.TB, root string) session {
	tb.Helper()
	binding := connectivityqualification.Binding{
		SessionID:    metadata.UUID("33333333-3333-4333-8333-333333333333"),
		BootID:       connectivity.FixtureBootID,
		CheckpointID: "cp-0001",
		SnapshotSHA256: "0123456789abcdef0123456789abcdef" +
			"0123456789abcdef0123456789abcdef",
		DiffSHA256: "1123456789abcdef0123456789abcdef" +
			"0123456789abcdef0123456789abcdef",
		ProposalsSHA256: "2123456789abcdef0123456789abcdef" +
			"0123456789abcdef0123456789abcdef",
	}
	recorder, err := connectivityqualification.OpenRecorder(root, binding)
	if err != nil {
		tb.Fatalf("open chain: %v", err)
	}
	return session{
		write: func(tb testing.TB) string {
			tb.Helper()
			record, err := recorder.Append(
				connectivityqualification.KindVerification,
				connectivityqualification.ResultObserved,
				"2026-09-01T00:00:00Z", 1,
				func(record *connectivityqualification.EvidenceRecord) {
					record.Verification = &connectivityqualification.Verification{
						Reproduced: 1}
				})
			if err != nil {
				tb.Fatalf("append evidence: %v", err)
			}
			return strconv.FormatUint(record.Sequence, 10)
		},
		seen: func(tb testing.TB) []string {
			tb.Helper()
			records, err := connectivityqualification.ReadRecords(root)
			if err != nil {
				tb.Fatalf("read evidence: %v", err)
			}
			held := make([]string, 0, len(records))
			for _, record := range records {
				held = append(held, strconv.FormatUint(record.Sequence, 10))
			}
			return held
		},
	}
}

// openWatchMemory starts a process over the transition watcher's memory.
//
// The watcher remembers one look rather than a log, so what must survive is
// the last one: a watcher that reopened blind would call the next look a first
// look and report every field as a transition.
func openWatchMemory(tb testing.TB, root string) session {
	tb.Helper()
	path := filepath.Join(root, "watch-state.json")
	// Load reports whether this is a first look, not whether it found
	// something: an absent memory is a first look, and a memory it holds is
	// not.
	load := func(tb testing.TB) (connectivitywatch.Facts, bool) {
		tb.Helper()
		facts, first, err := connectivitywatch.Load(path)
		if err != nil {
			tb.Fatalf("load watch memory: %v", err)
		}
		return facts, !first
	}
	return session{
		write: func(tb testing.TB) string {
			tb.Helper()
			facts, _ := load(tb)
			facts.Readable = true
			facts.OpenGaps++
			if err := connectivitywatch.Save(path, facts); err != nil {
				tb.Fatalf("save watch memory: %v", err)
			}
			return strconv.FormatUint(uint64(facts.OpenGaps), 10)
		},
		seen: func(tb testing.TB) []string {
			tb.Helper()
			facts, known := load(tb)
			if !known {
				return nil
			}
			return []string{strconv.FormatUint(uint64(facts.OpenGaps), 10)}
		},
	}
}
