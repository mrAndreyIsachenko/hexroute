package policystore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

func TestRetentionKeepsNewestValidGenerationsAndUnresolvedPrepares(t *testing.T) {
	store, path := newTestStore(t, policy.DomainRoot)
	defer store.Close()

	base := time.Date(2030, time.January, 1, 0, 10, 0, 0, time.UTC)
	unresolvedIntent := syntheticCommitIntent(t, syntheticTransactionID(1), 1)
	unresolvedReceipt := syntheticPrepareReceipt(t, unresolvedIntent, policy.DomainRoot)
	installSyntheticGeneration(t, store, unresolvedReceipt)
	if err := store.PersistPrepareReceipt(unresolvedReceipt); err != nil {
		t.Fatalf("persist unresolved receipt: %v", err)
	}

	var result RetentionResult
	for generation := uint64(2); generation <= 21; generation++ {
		intent := syntheticCommitIntent(t, syntheticTransactionID(generation), generation)
		receipt := syntheticPrepareReceipt(t, intent, policy.DomainRoot)
		pointer := syntheticActivePointer(t, intent, policy.DomainRoot)
		installSyntheticGeneration(t, store, receipt)
		if err := store.PersistPrepareReceipt(receipt); err != nil {
			t.Fatalf("persist receipt %d: %v", generation, err)
		}
		if err := store.PersistCommitIntent(intent); err != nil {
			t.Fatalf("persist commit %d: %v", generation, err)
		}
		if err := store.PersistActivePointer(pointer); err != nil {
			t.Fatalf("persist pointer %d: %v", generation, err)
		}
		resolvedAt := base.Add(time.Duration(generation) * time.Minute)
		var err error
		result, err = store.ResolveGeneration(
			syntheticResolution(receipt, policy.PolicyActive, policy.ReasonNone, resolvedAt),
			resolvedAt,
		)
		if err != nil {
			t.Fatalf("resolve generation %d: %v", generation, err)
		}
	}

	if result.RetainedValidGenerations != RetainedValidGenerations ||
		result.UnresolvedPrepares != 1 || result.AuditEntries != 20 {
		t.Fatalf("retention result = %+v", result)
	}
	for generation := uint64(1); generation <= 21; generation++ {
		wantRetained := generation == 1 || generation >= 6
		assertGenerationRetention(t, store, generation, wantRetained)
	}
	if _, err := store.ReadPrepareReceipt(unresolvedReceipt.TransactionID); err != nil {
		t.Fatalf("unresolved receipt was removed: %v", err)
	}
	for generation := uint64(2); generation <= 5; generation++ {
		transactionID := syntheticTransactionID(generation)
		if _, err := store.ReadPrepareReceipt(transactionID); !errors.Is(err, ErrRecordNotFound) {
			t.Fatalf("retired receipt %d error = %v", generation, err)
		}
		if _, err := store.ReadCommitIntent(transactionID); !errors.Is(err, ErrRecordNotFound) {
			t.Fatalf("retired commit %d error = %v", generation, err)
		}
		resolutionName := mustTransactionRecordFilename(t, recordResolution, transactionID)
		if _, err := os.Stat(filepath.Join(path, stateDirectory, resolutionName)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("retired resolution %d still present: %v", generation, err)
		}
	}
	index, err := store.ReadAuditIndex()
	if err != nil || len(index.Entries) != 20 {
		t.Fatalf("audit index = %+v, %v", index, err)
	}
}

func TestTerminalNonActiveResolutionRemovesCandidateBodiesAfterAudit(t *testing.T) {
	tests := []struct {
		name   string
		state  policy.PolicyState
		reason policy.PolicyReason
	}{
		{name: "rejected", state: policy.PolicyRejected, reason: policy.ReasonInvalidSignature},
		{name: "restart required", state: policy.PolicyRestartRequired, reason: policy.ReasonStaticMismatch},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, path := newTestStore(t, policy.DomainUser)
			defer store.Close()
			intent := syntheticCommitIntent(t, transactionOne, 9)
			receipt := syntheticPrepareReceipt(t, intent, policy.DomainUser)
			installSyntheticGeneration(t, store, receipt)
			if err := store.PersistPrepareReceipt(receipt); err != nil {
				t.Fatal(err)
			}
			resolvedAt := time.Date(2030, time.January, 2, 0, 0, 0, 0, time.UTC)
			result, err := store.ResolveGeneration(
				syntheticResolution(receipt, test.state, test.reason, resolvedAt),
				resolvedAt,
			)
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if result.AuditEntries != 1 || result.RemovedGenerationFiles != 4 ||
				result.RemovedStateRecords != 2 {
				t.Fatalf("retention result = %+v", result)
			}
			assertGenerationRetention(t, store, receipt.BundleGeneration, false)
			if _, err := store.ReadPrepareReceipt(receipt.TransactionID); !errors.Is(err, ErrRecordNotFound) {
				t.Fatalf("terminal receipt error = %v", err)
			}

			encoded, err := os.ReadFile(filepath.Join(path, stateDirectory, auditIndexFilename))
			if err != nil {
				t.Fatal(err)
			}
			for _, forbidden := range []string{
				string(receipt.TransactionID),
				"candidate-manifest-secret",
				"candidate-payload-secret",
				"candidate-review-secret",
				"candidate-approval-secret",
				intent.Approval.Signature,
			} {
				if strings.Contains(string(encoded), forbidden) {
					t.Fatalf("audit contains removed candidate evidence %q", forbidden)
				}
			}
			index, err := store.ReadAuditIndex()
			if err != nil || len(index.Entries) != 1 {
				t.Fatalf("audit index = %+v, %v", index, err)
			}
			entry := index.Entries[0]
			if entry.State != test.state || entry.Reason != test.reason ||
				entry.ManifestSHA256 != receipt.ManifestSHA256 ||
				entry.PayloadSHA256 != receipt.PayloadSHA256 {
				t.Fatalf("redacted audit entry = %+v", entry)
			}
		})
	}
}

func TestRetentionBoundsAuditByAgeCountAndEncodedSize(t *testing.T) {
	now := time.Date(2031, time.April, 1, 12, 0, 0, 0, time.UTC)
	digest := policy.SHA256Hex([]byte("audit-digest"))
	entry := func(generation uint64, recordedAt time.Time) AuditEntry {
		return AuditEntry{
			Domain: policy.DomainRoot, State: policy.PolicyRejected,
			BundleGeneration: generation, PolicyGeneration: generation,
			ManifestSHA256: digest, PayloadSHA256: digest,
			RecordedAt: recordedAt.UTC().Format(time.RFC3339Nano),
			Reason:     policy.ReasonSelectorConflict,
		}
	}

	t.Run("ninety day boundary", func(t *testing.T) {
		index := AuditIndex{Schema: AuditIndexSchema, Entries: []AuditEntry{
			entry(1, now.Add(-AuditRetention-time.Nanosecond)),
			entry(2, now.Add(-AuditRetention)),
			entry(3, now),
		}}
		result, err := mergeAndPruneAudit(index, nil, now)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Entries) != 2 || result.Entries[0].BundleGeneration != 2 ||
			result.Entries[1].BundleGeneration != 3 {
			t.Fatalf("age-bounded audit = %+v", result.Entries)
		}
	})

	t.Run("entry and byte bounds", func(t *testing.T) {
		entries := make([]AuditEntry, 0, MaxAuditEntries+12)
		for generation := uint64(1); generation <= MaxAuditEntries+12; generation++ {
			entries = append(entries, entry(generation, now.Add(-time.Duration(MaxAuditEntries+12-generation)*time.Minute)))
		}
		result, err := mergeAndPruneAudit(
			AuditIndex{Schema: AuditIndexSchema, Entries: entries}, nil, now,
		)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Entries) != MaxAuditEntries || result.Entries[0].BundleGeneration != 13 ||
			result.Entries[len(result.Entries)-1].BundleGeneration != MaxAuditEntries+12 {
			t.Fatalf("count-bounded audit range = %d..%d (%d entries)",
				result.Entries[0].BundleGeneration,
				result.Entries[len(result.Entries)-1].BundleGeneration,
				len(result.Entries),
			)
		}
		encoded, err := marshalRecord(result)
		if err != nil || len(encoded) > MaxRecordSize {
			t.Fatalf("bounded audit encoded bytes = %d, %v", len(encoded), err)
		}
	})
}

func TestResolutionTransitionsAndAuditDomainFailClosed(t *testing.T) {
	t.Run("active requires matching pointer", func(t *testing.T) {
		store, path := newTestStore(t, policy.DomainRoot)
		defer store.Close()
		intent := syntheticCommitIntent(t, transactionOne, 3)
		receipt := syntheticPrepareReceipt(t, intent, policy.DomainRoot)
		if err := store.PersistPrepareReceipt(receipt); err != nil {
			t.Fatal(err)
		}
		resolvedAt := time.Date(2030, time.January, 3, 0, 0, 0, 0, time.UTC)
		_, err := store.ResolveGeneration(
			syntheticResolution(receipt, policy.PolicyActive, policy.ReasonNone, resolvedAt), resolvedAt,
		)
		if !errors.Is(err, ErrRecordConflict) {
			t.Fatalf("active resolution without pointer error = %v", err)
		}
		name := mustTransactionRecordFilename(t, recordResolution, transactionOne)
		if _, statErr := os.Stat(filepath.Join(path, stateDirectory, name)); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("conflicting resolution was persisted: %v", statErr)
		}
	})

	t.Run("current active cannot be rejected", func(t *testing.T) {
		store, _ := newTestStore(t, policy.DomainRoot)
		defer store.Close()
		intent := syntheticCommitIntent(t, transactionOne, 4)
		receipt := syntheticPrepareReceipt(t, intent, policy.DomainRoot)
		if err := store.PersistPrepareReceipt(receipt); err != nil {
			t.Fatal(err)
		}
		if err := store.PersistCommitIntent(intent); err != nil {
			t.Fatal(err)
		}
		if err := store.PersistActivePointer(syntheticActivePointer(t, intent, policy.DomainRoot)); err != nil {
			t.Fatal(err)
		}
		resolvedAt := time.Date(2030, time.January, 3, 0, 1, 0, 0, time.UTC)
		_, err := store.ResolveGeneration(
			syntheticResolution(receipt, policy.PolicyRejected, policy.ReasonDigestMismatch, resolvedAt), resolvedAt,
		)
		if !errors.Is(err, ErrRecordConflict) {
			t.Fatalf("rejected current pointer error = %v", err)
		}
	})

	t.Run("foreign audit domain", func(t *testing.T) {
		store, path := newTestStore(t, policy.DomainRoot)
		defer store.Close()
		digest := policy.SHA256Hex([]byte("foreign-audit"))
		index := AuditIndex{Schema: AuditIndexSchema, Entries: []AuditEntry{{
			Domain: policy.DomainUser, State: policy.PolicyRejected,
			BundleGeneration: 1, PolicyGeneration: 1,
			ManifestSHA256: digest, PayloadSHA256: digest,
			RecordedAt: "2030-01-01T00:00:00Z", Reason: policy.ReasonDigestMismatch,
		}}}
		encoded, err := marshalRecord(index)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(
			filepath.Join(path, stateDirectory, auditIndexFilename), encoded, RecordFileMode,
		); err != nil {
			t.Fatal(err)
		}
		if _, err := store.ReadAuditIndex(); !errors.Is(err, ErrInvalidRecord) {
			t.Fatalf("foreign audit domain error = %v", err)
		}
	})
}

func TestRetentionRemovesInterruptedPersistenceTemporaryFiles(t *testing.T) {
	store, path := newTestStore(t, policy.DomainRoot)
	defer store.Close()
	intent := syntheticCommitIntent(t, transactionOne, 2)
	receipt := syntheticPrepareReceipt(t, intent, policy.DomainRoot)
	store.persistenceFault = func(operation recordOperation, boundary persistenceBoundary) error {
		if operation == recordPrepare && boundary == boundaryBeforeRename {
			return errors.New("synthetic crash")
		}
		return nil
	}
	if err := store.PersistPrepareReceipt(receipt); !errors.Is(err, ErrPersistenceInterrupted) {
		t.Fatalf("interrupted receipt error = %v", err)
	}
	store.persistenceFault = nil
	before, err := os.ReadDir(filepath.Join(path, stateDirectory))
	if err != nil {
		t.Fatal(err)
	}
	foundTemporary := false
	for _, entry := range before {
		foundTemporary = foundTemporary || isTransientRecordName(entry.Name())
	}
	if !foundTemporary {
		t.Fatal("expected an interrupted persistence temporary file")
	}

	result, err := store.ApplyRetention(time.Date(2030, time.January, 4, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if result.RemovedTransientFiles != 1 {
		t.Fatalf("retention result = %+v", result)
	}
	after, err := os.ReadDir(filepath.Join(path, stateDirectory))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range after {
		if isTransientRecordName(entry.Name()) {
			t.Fatalf("temporary file remains: %s", entry.Name())
		}
	}
}

func TestRejectedCandidateSurvivesInterruptedAuditUntilDurableRetry(t *testing.T) {
	store, path := newTestStore(t, policy.DomainRoot)
	intent := syntheticCommitIntent(t, transactionOne, 5)
	receipt := syntheticPrepareReceipt(t, intent, policy.DomainRoot)
	installSyntheticGeneration(t, store, receipt)
	if err := store.PersistPrepareReceipt(receipt); err != nil {
		t.Fatal(err)
	}
	store.persistenceFault = func(operation recordOperation, boundary persistenceBoundary) error {
		if operation == recordAudit && boundary == boundaryBeforeDirectorySync {
			return errors.New("synthetic crash before durable audit")
		}
		return nil
	}
	resolvedAt := time.Date(2030, time.January, 5, 0, 0, 0, 0, time.UTC)
	resolution := syntheticResolution(
		receipt, policy.PolicyRejected, policy.ReasonUnsupportedSchema, resolvedAt,
	)
	if _, err := store.ResolveGeneration(resolution, resolvedAt); !errors.Is(err, ErrPersistenceInterrupted) {
		t.Fatalf("interrupted audit error = %v", err)
	}
	assertGenerationRetention(t, store, receipt.BundleGeneration, true)
	if _, err := store.ReadPrepareReceipt(receipt.TransactionID); err != nil {
		t.Fatalf("receipt removed before durable audit: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	var err error
	store, err = openStoreAt(path, policy.DomainRoot, currentUID(), currentGID())
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer store.Close()
	result, err := store.ResolveGeneration(resolution, resolvedAt)
	if err != nil {
		t.Fatalf("durable retry: %v", err)
	}
	if result.AuditEntries != 1 || result.RemovedGenerationFiles != 4 {
		t.Fatalf("retry retention result = %+v", result)
	}
	assertGenerationRetention(t, store, receipt.BundleGeneration, false)
}

func syntheticTransactionID(value uint64) metadata.UUID {
	return metadata.UUID(fmt.Sprintf("%08x-0000-4000-8000-%012x", value, value))
}

func syntheticResolution(
	receipt PrepareReceipt,
	state policy.PolicyState,
	reason policy.PolicyReason,
	resolvedAt time.Time,
) ResolutionRecord {
	return ResolutionRecord{
		Schema: ResolutionSchema, TransactionID: receipt.TransactionID, Domain: receipt.Domain,
		State: state, BundleGeneration: receipt.BundleGeneration,
		PolicyGeneration: receipt.PolicyGeneration, ManifestSHA256: receipt.ManifestSHA256,
		PayloadSHA256: receipt.PayloadSHA256,
		ResolvedAt:    resolvedAt.UTC().Format(time.RFC3339Nano), Reason: reason,
	}
}

func installSyntheticGeneration(t *testing.T, store *Store, receipt PrepareReceipt) {
	t.Helper()
	generation := Generation{Bundle: receipt.BundleGeneration, Policy: receipt.PolicyGeneration}
	contents := map[ArtifactKind]string{
		ArtifactManifest: "candidate-manifest-secret",
		ArtifactPayload:  "candidate-payload-secret",
		ArtifactReview:   "candidate-review-secret",
		ArtifactApproval: "candidate-approval-secret",
	}
	for _, kind := range []ArtifactKind{ArtifactManifest, ArtifactPayload, ArtifactReview, ArtifactApproval} {
		if err := store.InstallArtifact(generation, kind, []byte(contents[kind])); err != nil {
			t.Fatalf("install %s for %+v: %v", kind, generation, err)
		}
	}
}

func assertGenerationRetention(t *testing.T, store *Store, generation uint64, wantRetained bool) {
	t.Helper()
	value := Generation{Bundle: generation, Policy: generation}
	for _, kind := range []ArtifactKind{ArtifactManifest, ArtifactPayload, ArtifactReview, ArtifactApproval} {
		_, err := store.ReadArtifact(value, kind)
		if wantRetained && err != nil {
			t.Fatalf("retained generation %d/%s: %v", generation, kind, err)
		}
		if !wantRetained && !errors.Is(err, ErrGenerationNotFound) {
			t.Fatalf("removed generation %d/%s error = %v", generation, kind, err)
		}
	}
}
