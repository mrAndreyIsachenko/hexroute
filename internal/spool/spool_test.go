package spool

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/event"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const testNodeID = metadata.UUID("11111111-1111-4111-8111-111111111111")

type fixedSpoolClock struct{}

func (fixedSpoolClock) WallNow() time.Time {
	return time.Date(2026, time.July, 23, 18, 30, 0, 0, time.UTC)
}

func (fixedSpoolClock) MonotonicNow() time.Duration {
	return 0
}

func TestRootAndUserSpoolsAreOwnerSeparatedAndPrivate(t *testing.T) {
	rootPath := filepath.Join(t.TempDir(), "root")
	userPath := filepath.Join(t.TempDir(), "user")

	rootSpool, err := Open(rootPath, OwnerRoot, testOptions(0))
	if err != nil {
		t.Fatalf("Open(root) error = %v", err)
	}
	userSpool, err := Open(userPath, OwnerUser, testOptions(0))
	if err != nil {
		t.Fatalf("Open(user) error = %v", err)
	}
	if rootSpool.maxBytes != DefaultMaxBytes || userSpool.maxBytes != DefaultMaxBytes {
		t.Fatal("default spool cap is not 100 MiB")
	}
	if _, err := Open(rootPath, OwnerUser, testOptions(0)); !errors.Is(err, ErrOwnerMismatch) {
		t.Fatalf("Open(root as user) error = %v, want %v", err, ErrOwnerMismatch)
	}

	for _, path := range []string{rootPath, userPath} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Fatalf("%s mode = %o, want 700", path, info.Mode().Perm())
		}
		markerInfo, err := os.Stat(filepath.Join(path, ownerFilename))
		if err != nil {
			t.Fatalf("stat owner marker: %v", err)
		}
		if markerInfo.Mode().Perm() != 0o600 {
			t.Fatalf("owner marker mode = %o, want 600", markerInfo.Mode().Perm())
		}
	}
}

func TestEvictionOrderPreservesCriticalRecords(t *testing.T) {
	critical := mustIncident(t, "inc-critical")
	diagnostic := mustDiagnostic(t)
	operational := mustObservation(t)
	incoming := mustTransition(t, 4)

	criticalEntry := mustEntry(t, 1, critical)
	diagnosticEntry := mustEntry(t, 2, diagnostic)
	operationalEntry := mustEntry(t, 3, operational)
	incomingEntry := mustEntry(t, 4, incoming)
	maxBytes := criticalEntry.Size + diagnosticEntry.Size + operationalEntry.Size +
		incomingEntry.Size - diagnosticEntry.Size

	spool, err := Open(
		filepath.Join(t.TempDir(), "spool"),
		OwnerRoot,
		testOptions(maxBytes),
	)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	mustAppend(t, spool, critical)
	mustAppend(t, spool, diagnostic)
	mustAppend(t, spool, operational)
	mustAppend(t, spool, incoming)

	entries, err := spool.Entries()
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}
	assertSequences(t, entries, []uint64{1, 3, 4})
	if entries[0].Priority != event.PriorityCritical ||
		entries[1].Priority != event.PriorityOperational {
		t.Fatalf("unexpected priorities after eviction: %+v", entries)
	}
	assertWithinCap(t, spool)
}

func TestOperationalEvictsBeforeCritical(t *testing.T) {
	critical := mustIncident(t, "inc-critical")
	operational := mustObservation(t)
	incoming := mustTransition(t, 3)

	criticalEntry := mustEntry(t, 1, critical)
	operationalEntry := mustEntry(t, 2, operational)
	incomingEntry := mustEntry(t, 3, incoming)
	maxBytes := criticalEntry.Size + operationalEntry.Size + incomingEntry.Size -
		operationalEntry.Size

	spool, err := Open(
		filepath.Join(t.TempDir(), "spool"),
		OwnerRoot,
		testOptions(maxBytes),
	)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	mustAppend(t, spool, critical)
	mustAppend(t, spool, operational)
	mustAppend(t, spool, incoming)

	entries, err := spool.Entries()
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}
	assertSequences(t, entries, []uint64{1, 3})
	assertWithinCap(t, spool)
}

func TestCriticalOverflowPreservesNewestChainAndRecordsIncident(t *testing.T) {
	first := mustDeployment(t)
	second := mustIncident(t, "inc-second")
	incoming := mustTransition(t, 3)
	overflow := mustOverflowEntry(t, 4)

	firstEntry := mustEntry(t, 1, first)
	secondEntry := mustEntry(t, 2, second)
	incomingEntry := mustEntry(t, 3, incoming)
	maxBytes := secondEntry.Size + incomingEntry.Size + overflow.Size
	if maxBytes >= firstEntry.Size+secondEntry.Size+incomingEntry.Size+overflow.Size {
		t.Fatal("test limit does not force critical eviction")
	}

	spool, err := Open(
		filepath.Join(t.TempDir(), "spool"),
		OwnerRoot,
		testOptions(maxBytes),
	)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	mustAppend(t, spool, first)
	mustAppend(t, spool, second)
	mustAppend(t, spool, incoming)

	entries, err := spool.Entries()
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}
	assertSequences(t, entries, []uint64{2, 3, 4})
	last, err := event.Decode(entries[len(entries)-1].Event)
	if err != nil {
		t.Fatalf("decode overflow event: %v", err)
	}
	incident, ok := last.Payload.(*event.Incident)
	if !ok || incident.Category != event.IncidentSpoolOverflow ||
		incident.Component != control.ComponentRuntime {
		t.Fatalf("overflow payload = %#v", last.Payload)
	}
	assertWithinCap(t, spool)
}

func TestAppendAfterAcknowledgementKeepsMonotonicSequence(t *testing.T) {
	spool, err := Open(
		filepath.Join(t.TempDir(), "spool"),
		OwnerRoot,
		testOptions(4096),
	)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	mustAppend(t, spool, mustObservation(t))
	mustAppend(t, spool, mustTransition(t, 2))
	entries, err := spool.Entries()
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}
	_, err = spool.Acknowledge([]metadata.UUID{
		entries[0].Metadata.EventID,
		entries[1].Metadata.EventID,
	})
	if err != nil {
		t.Fatalf("Acknowledge() error = %v", err)
	}

	sequence := mustAppend(t, spool, mustDiagnostic(t))
	if sequence != 3 {
		t.Fatalf("post-ack sequence = %d, want 3", sequence)
	}
	entries, err = spool.Entries()
	if err != nil {
		t.Fatalf("Entries(after append) error = %v", err)
	}
	assertSequences(t, entries, []uint64{3})
}

func mustDeployment(t *testing.T) []byte {
	t.Helper()
	encoded, err := event.Encode(event.SchemaDeployment, event.Deployment{
		DeploymentID: "deployment-20260723T173500Z-primary-control-plane",
		Release:      "release-v0.1.0-observe-only-initial-workspace",
		Status:       event.DeploymentActivated,
		DigestSHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	})
	if err != nil {
		t.Fatalf("encode deployment: %v", err)
	}
	return encoded
}

func TestCrashRecoveryPromotesCompletePendingAndRemovesPartial(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spool")
	spool, err := Open(path, OwnerUser, testOptions(4096))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	complete := mustEntry(t, 1, mustObservation(t))
	if err := spool.stage(complete); err != nil {
		t.Fatalf("stage complete pending record: %v", err)
	}
	partialPath := filepath.Join(path, pendingName(2))
	if err := os.WriteFile(partialPath, []byte(`{"schema":`), 0o600); err != nil {
		t.Fatalf("write partial pending record: %v", err)
	}

	recovered, err := Open(path, OwnerUser, testOptions(4096))
	if err != nil {
		t.Fatalf("Open(recover) error = %v", err)
	}
	entries, err := recovered.Entries()
	if err != nil {
		t.Fatalf("Entries() error = %v", err)
	}
	assertSequences(t, entries, []uint64{1})
	if _, err := os.Stat(partialPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial pending record still exists: %v", err)
	}
}

func TestCorruptStableRecordStopsRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "spool")
	if _, err := Open(path, OwnerRoot, testOptions(4096)); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(path, stableName(1)),
		[]byte(`{"schema":"broken"}`),
		0o600,
	); err != nil {
		t.Fatalf("write corrupt record: %v", err)
	}
	if _, err := Open(path, OwnerRoot, testOptions(4096)); !errors.Is(err, ErrCorruptSpool) {
		t.Fatalf("Open(corrupt) error = %v, want %v", err, ErrCorruptSpool)
	}
}

func mustAppend(t *testing.T, spool *Spool, encoded []byte) uint64 {
	t.Helper()
	sequence, err := spool.Append(encoded)
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	return sequence
}

func mustEntry(t *testing.T, sequence uint64, encoded []byte) Entry {
	t.Helper()
	record, err := event.Decode(encoded)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	entry, err := newEntry(testMetadata(sequence), record.Priority, encoded)
	if err != nil {
		t.Fatalf("newEntry() error = %v", err)
	}
	return entry
}

func mustOverflowEntry(t *testing.T, sequence uint64) Entry {
	t.Helper()
	encoded, err := event.Encode(event.SchemaIncident, event.Incident{
		IncidentID: "spool-overflow-" + string(rune('0'+sequence)),
		Status:     event.IncidentOpened,
		Severity:   event.SeverityCritical,
		Category:   event.IncidentSpoolOverflow,
		Component:  control.ComponentRuntime,
		Generation: sequence,
	})
	if err != nil {
		t.Fatalf("encode overflow event: %v", err)
	}
	return mustEntry(t, sequence, encoded)
}

func mustIncident(t *testing.T, id string) []byte {
	t.Helper()
	encoded, err := event.Encode(event.SchemaIncident, event.Incident{
		IncidentID: id,
		Status:     event.IncidentOpened,
		Severity:   event.SeverityCritical,
		Category:   event.IncidentAvailability,
		Component:  control.ComponentTunnel,
		Generation: 1,
	})
	if err != nil {
		t.Fatalf("encode incident: %v", err)
	}
	return encoded
}

func mustDiagnostic(t *testing.T) []byte {
	t.Helper()
	encoded, err := event.Encode(event.SchemaDiagnostic, event.Diagnostic{
		Component:  control.ComponentRuntime,
		Code:       event.DiagnosticAdapterSampled,
		Count:      1,
		DurationMS: 5,
	})
	if err != nil {
		t.Fatalf("encode diagnostic: %v", err)
	}
	return encoded
}

func mustObservation(t *testing.T) []byte {
	t.Helper()
	encoded, err := event.Encode(event.SchemaObservation, event.Observation{
		Component: control.ComponentNetwork,
		Health:    control.HealthReady,
		Reason:    control.ReasonProbeSucceeded,
	})
	if err != nil {
		t.Fatalf("encode observation: %v", err)
	}
	return encoded
}

func mustTransition(t *testing.T, generation uint64) []byte {
	t.Helper()
	encoded, err := event.Encode(event.SchemaTransition, event.Transition{
		Component:  control.ComponentTunnel,
		From:       control.StateRecovering,
		To:         control.StateHealthy,
		Reason:     control.ReasonVerificationPassed,
		Generation: generation,
	})
	if err != nil {
		t.Fatalf("encode transition: %v", err)
	}
	return encoded
}

func assertSequences(t *testing.T, entries []Entry, want []uint64) {
	t.Helper()
	got := make([]uint64, 0, len(entries))
	for _, entry := range entries {
		got = append(got, entry.Sequence)
	}
	encodedGot, _ := json.Marshal(got)
	encodedWant, _ := json.Marshal(want)
	if string(encodedGot) != string(encodedWant) {
		t.Fatalf("sequences = %s, want %s", encodedGot, encodedWant)
	}
}

func assertWithinCap(t *testing.T, spool *Spool) {
	t.Helper()
	size, err := spool.Size()
	if err != nil {
		t.Fatalf("Size() error = %v", err)
	}
	if size > spool.maxBytes {
		t.Fatalf("spool size = %d, cap = %d", size, spool.maxBytes)
	}
}

func testOptions(maxBytes int64) Options {
	return Options{
		MaxBytes: maxBytes,
		NodeID:   testNodeID,
		Clock:    fixedSpoolClock{},
	}
}

func testMetadata(sequence uint64) metadata.Metadata {
	return metadata.Metadata{
		EventID: metadata.UUID(fmt.Sprintf(
			"22222222-2222-4222-8222-%012d",
			sequence,
		)),
		NodeID:         testNodeID,
		SessionID:      metadata.UUID("33333333-3333-4333-8333-333333333333"),
		Sequence:       sequence,
		WallClock:      time.Date(2026, time.July, 23, 18, 30, 0, 0, time.UTC),
		MonotonicNanos: int64(sequence),
	}
}
