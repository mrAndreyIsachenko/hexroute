package policystore

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
	"github.com/mrAndreyIsachenko/hexroute/internal/policyapproval"
)

const (
	transactionOne = metadata.UUID("11111111-1111-4111-8111-111111111111")
	transactionTwo = metadata.UUID("22222222-2222-4222-8222-222222222222")
	preparedAt     = "2030-01-01T00:01:00Z"
	createdAt      = "2030-01-01T00:02:00Z"
	activatedAt    = "2030-01-01T00:03:00Z"
)

func TestStorePersistsTypedTransactionEvidenceAndActivePointer(t *testing.T) {
	store, path := newTestStore(t, policy.DomainRoot)
	defer store.Close()

	intent := syntheticCommitIntent(t, transactionOne, 7)
	receipt := syntheticPrepareReceipt(t, intent, policy.DomainRoot)
	pointer := syntheticActivePointer(t, intent, policy.DomainRoot)

	if err := store.PersistPrepareReceipt(receipt); err != nil {
		t.Fatalf("persist prepare receipt: %v", err)
	}
	if err := store.PersistCommitIntent(intent); err != nil {
		t.Fatalf("persist commit intent: %v", err)
	}
	if err := store.PersistActivePointer(pointer); err != nil {
		t.Fatalf("persist active pointer: %v", err)
	}

	readReceipt, err := store.ReadPrepareReceipt(transactionOne)
	if err != nil || !reflect.DeepEqual(readReceipt, receipt) {
		t.Fatalf("read prepare receipt = %+v, %v", readReceipt, err)
	}
	readIntent, err := store.ReadCommitIntent(transactionOne)
	if err != nil || !reflect.DeepEqual(readIntent, intent) {
		t.Fatalf("read commit intent = %+v, %v", readIntent, err)
	}
	readPointer, err := store.ReadActivePointer()
	if err != nil || !reflect.DeepEqual(readPointer, pointer) {
		t.Fatalf("read active pointer = %+v, %v", readPointer, err)
	}

	// Durable retries of byte-identical evidence are idempotent.
	if err := store.PersistPrepareReceipt(receipt); err != nil {
		t.Fatalf("retry prepare receipt: %v", err)
	}
	if err := store.PersistCommitIntent(intent); err != nil {
		t.Fatalf("retry commit intent: %v", err)
	}
	if err := store.PersistActivePointer(pointer); err != nil {
		t.Fatalf("retry active pointer: %v", err)
	}

	for _, name := range []string{
		mustTransactionRecordFilename(t, recordPrepare, transactionOne),
		mustTransactionRecordFilename(t, recordCommit, transactionOne),
		activePointerFilename,
	} {
		assertPrivateRecord(t, filepath.Join(path, stateDirectory, name))
	}
	assertPrivateDirectory(t, filepath.Join(path, stateDirectory))

	newerIntent := syntheticCommitIntent(t, transactionTwo, 8)
	newerReceipt := syntheticPrepareReceipt(t, newerIntent, policy.DomainRoot)
	if err := store.PersistPrepareReceipt(newerReceipt); err != nil {
		t.Fatalf("persist newer receipt: %v", err)
	}
	if err := store.PersistCommitIntent(newerIntent); err != nil {
		t.Fatalf("persist newer intent: %v", err)
	}
	newerPointer := syntheticActivePointer(t, newerIntent, policy.DomainRoot)
	if err := store.PersistActivePointer(newerPointer); err != nil {
		t.Fatalf("advance active pointer: %v", err)
	}
	readPointer, err = store.ReadActivePointer()
	if err != nil || !reflect.DeepEqual(readPointer, newerPointer) {
		t.Fatalf("advanced active pointer = %+v, %v", readPointer, err)
	}
	if err := store.PersistActivePointer(pointer); !errors.Is(err, ErrStaleActivePointer) {
		t.Fatalf("stale active pointer error = %v", err)
	}
}

func TestImmutableTransactionEvidenceRejectsConflictingRetry(t *testing.T) {
	store, _ := newTestStore(t, policy.DomainUser)
	defer store.Close()
	intent := syntheticCommitIntent(t, transactionOne, 3)
	receipt := syntheticPrepareReceipt(t, intent, policy.DomainUser)

	if err := store.PersistPrepareReceipt(receipt); err != nil {
		t.Fatal(err)
	}
	conflictingReceipt := receipt
	conflictingReceipt.PreparedAt = "2030-01-01T00:01:01Z"
	if err := store.PersistPrepareReceipt(conflictingReceipt); !errors.Is(err, ErrRecordConflict) {
		t.Fatalf("conflicting receipt error = %v", err)
	}

	if err := store.PersistCommitIntent(intent); err != nil {
		t.Fatal(err)
	}
	conflictingIntent := intent
	conflictingIntent.CreatedAt = "2030-01-01T00:02:01Z"
	if err := store.PersistCommitIntent(conflictingIntent); !errors.Is(err, ErrRecordConflict) {
		t.Fatalf("conflicting intent error = %v", err)
	}

	readReceipt, err := store.ReadPrepareReceipt(transactionOne)
	if err != nil || !reflect.DeepEqual(readReceipt, receipt) {
		t.Fatalf("immutable receipt changed: %+v, %v", readReceipt, err)
	}
	readIntent, err := store.ReadCommitIntent(transactionOne)
	if err != nil || !reflect.DeepEqual(readIntent, intent) {
		t.Fatalf("immutable intent changed: %+v, %v", readIntent, err)
	}
}

func TestStoreEnforcesLocalPrepareCommitActivateOrder(t *testing.T) {
	store, _ := newTestStore(t, policy.DomainRoot)
	defer store.Close()
	intent := syntheticCommitIntent(t, transactionOne, 5)
	receipt := syntheticPrepareReceipt(t, intent, policy.DomainRoot)
	pointer := syntheticActivePointer(t, intent, policy.DomainRoot)

	if err := store.PersistCommitIntent(intent); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("commit without prepare error = %v", err)
	}
	if err := store.PersistActivePointer(pointer); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("activate without commit error = %v", err)
	}
	if err := store.PersistPrepareReceipt(receipt); err != nil {
		t.Fatal(err)
	}
	mismatched := intent
	mismatched.BundleGeneration++
	if err := store.PersistCommitIntent(mismatched); !errors.Is(err, ErrRecordConflict) {
		t.Fatalf("mismatched commit error = %v", err)
	}
	if err := store.PersistCommitIntent(intent); err != nil {
		t.Fatal(err)
	}
	mismatchedPointer := pointer
	mismatchedPointer.CommitIntentSHA256 = strings.Repeat("f", 64)
	if err := store.PersistActivePointer(mismatchedPointer); !errors.Is(err, ErrRecordConflict) {
		t.Fatalf("mismatched active pointer error = %v", err)
	}
	if err := store.PersistActivePointer(pointer); err != nil {
		t.Fatal(err)
	}
}

func TestStoreRejectsInvalidStateRecords(t *testing.T) {
	store, _ := newTestStore(t, policy.DomainRoot)
	defer store.Close()
	intent := syntheticCommitIntent(t, transactionOne, 4)
	receipt := syntheticPrepareReceipt(t, intent, policy.DomainRoot)
	pointer := syntheticActivePointer(t, intent, policy.DomainRoot)

	invalidReceipt := receipt
	invalidReceipt.TransactionID = metadata.UUID("../receipt")
	if err := store.PersistPrepareReceipt(invalidReceipt); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("invalid receipt error = %v", err)
	}
	wrongDomain := receipt
	wrongDomain.Domain = policy.DomainUser
	if err := store.PersistPrepareReceipt(wrongDomain); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("wrong-domain receipt error = %v", err)
	}
	invalidIntent := intent
	invalidIntent.Approval.Signature = "invalid"
	if err := store.PersistCommitIntent(invalidIntent); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("invalid commit intent error = %v", err)
	}
	invalidPointer := pointer
	invalidPointer.PayloadSHA256 = strings.Repeat("f", 64)
	if err := store.PersistActivePointer(invalidPointer); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("invalid active pointer error = %v", err)
	}
	if _, err := store.ReadPrepareReceipt(transactionTwo); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("missing receipt error = %v", err)
	}
	if _, err := store.ReadCommitIntent(metadata.UUID("not-a-uuid")); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("invalid transaction lookup error = %v", err)
	}
	if _, err := store.ReadActivePointer(); !errors.Is(err, ErrRecordNotFound) {
		t.Fatalf("missing active pointer error = %v", err)
	}
}

func TestStoreRejectsReplacedOrInsecureStateDirectory(t *testing.T) {
	t.Run("symlink on open", func(t *testing.T) {
		store, path := newTestStore(t, policy.DomainRoot)
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		statePath := filepath.Join(path, stateDirectory)
		moved := statePath + "-moved"
		if err := os.Rename(statePath, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(moved, statePath); err != nil {
			t.Fatal(err)
		}
		if _, err := openStoreAt(path, policy.DomainRoot, currentUID(), currentGID()); !errors.Is(err, ErrInsecureStore) {
			t.Fatalf("symlinked state directory error = %v", err)
		}
	})

	t.Run("replacement after open", func(t *testing.T) {
		store, path := newTestStore(t, policy.DomainRoot)
		defer store.Close()
		statePath := filepath.Join(path, stateDirectory)
		moved := statePath + "-moved"
		if err := os.Rename(statePath, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(statePath, DirectoryMode); err != nil {
			t.Fatal(err)
		}
		intent := syntheticCommitIntent(t, transactionOne, 2)
		if err := store.PersistCommitIntent(intent); !errors.Is(err, ErrInsecureStore) {
			t.Fatalf("replaced state directory error = %v", err)
		}
	})
}

func TestStoreRejectsSymlinkModeAndHardLinkStateRecords(t *testing.T) {
	mutations := []struct {
		name   string
		mutate func(*testing.T, string)
	}{
		{
			name: "symlink",
			mutate: func(t *testing.T, path string) {
				target := path + ".target"
				if err := os.WriteFile(target, []byte("target"), RecordFileMode); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "writable mode",
			mutate: func(t *testing.T, path string) {
				if err := os.Chmod(path, 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "hard link",
			mutate: func(t *testing.T, path string) {
				if err := os.Link(path, path+".link"); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			store, path := newTestStore(t, policy.DomainUser)
			defer store.Close()
			intent := syntheticCommitIntent(t, transactionOne, 2)
			receipt := syntheticPrepareReceipt(t, intent, policy.DomainUser)
			if err := store.PersistPrepareReceipt(receipt); err != nil {
				t.Fatal(err)
			}
			if err := store.PersistCommitIntent(intent); err != nil {
				t.Fatal(err)
			}
			name := mustTransactionRecordFilename(t, recordCommit, transactionOne)
			mutation.mutate(t, filepath.Join(path, stateDirectory, name))
			if _, err := store.ReadCommitIntent(transactionOne); !errors.Is(err, ErrInsecureRecord) {
				t.Fatalf("insecure record error = %v", err)
			}
			if err := store.PersistCommitIntent(intent); !errors.Is(err, ErrInsecureRecord) {
				t.Fatalf("insecure record retry error = %v", err)
			}
		})
	}
}

func TestPersistenceCrashBoundariesRecoverToOldOrNewCompleteRecord(t *testing.T) {
	boundaries := []persistenceBoundary{
		boundaryBeforeFileSync,
		boundaryAfterFileSync,
		boundaryBeforeRename,
		boundaryAfterRename,
		boundaryBeforeDirectorySync,
		boundaryAfterDirectorySync,
	}
	operations := []recordOperation{recordPrepare, recordCommit, recordActive}
	for _, operation := range operations {
		for _, boundary := range boundaries {
			t.Run(string(operation)+"/"+string(boundary), func(t *testing.T) {
				store, path := newTestStore(t, policy.DomainRoot)
				oldIntent := syntheticCommitIntent(t, transactionOne, 1)
				oldReceipt := syntheticPrepareReceipt(t, oldIntent, policy.DomainRoot)
				oldPointer := syntheticActivePointer(t, oldIntent, policy.DomainRoot)
				if operation == recordActive {
					if err := store.PersistPrepareReceipt(oldReceipt); err != nil {
						t.Fatal(err)
					}
					if err := store.PersistCommitIntent(oldIntent); err != nil {
						t.Fatal(err)
					}
					if err := store.PersistActivePointer(oldPointer); err != nil {
						t.Fatal(err)
					}
				}

				newIntent := syntheticCommitIntent(t, transactionTwo, 2)
				newReceipt := syntheticPrepareReceipt(t, newIntent, policy.DomainRoot)
				newPointer := syntheticActivePointer(t, newIntent, policy.DomainRoot)
				if operation == recordCommit || operation == recordActive {
					if err := store.PersistPrepareReceipt(newReceipt); err != nil {
						t.Fatal(err)
					}
				}
				if operation == recordActive {
					if err := store.PersistCommitIntent(newIntent); err != nil {
						t.Fatal(err)
					}
				}
				triggered := false
				store.persistenceFault = func(gotOperation recordOperation, gotBoundary persistenceBoundary) error {
					if gotOperation == operation && gotBoundary == boundary {
						triggered = true
						return errors.New("synthetic crash")
					}
					return nil
				}
				err := persistSyntheticOperation(store, operation, newReceipt, newIntent, newPointer)
				if !errors.Is(err, ErrPersistenceInterrupted) || !triggered {
					t.Fatalf("fault result = %v, triggered=%v", err, triggered)
				}
				if err := store.Close(); err != nil {
					t.Fatal(err)
				}
				store, err = openStoreAt(path, policy.DomainRoot, currentUID(), currentGID())
				if err != nil {
					t.Fatalf("reopen store: %v", err)
				}
				defer store.Close()

				renamed := boundary == boundaryAfterRename ||
					boundary == boundaryBeforeDirectorySync || boundary == boundaryAfterDirectorySync
				assertCrashOutcome(t, store, operation, renamed, oldPointer, newReceipt, newIntent, newPointer)

				// Retrying after either side of an ambiguous boundary converges to the
				// same complete record without overwriting immutable evidence.
				if err := persistSyntheticOperation(store, operation, newReceipt, newIntent, newPointer); err != nil {
					t.Fatalf("retry after simulated crash: %v", err)
				}
				assertCrashOutcome(t, store, operation, true, oldPointer, newReceipt, newIntent, newPointer)
			})
		}
	}
}

func persistSyntheticOperation(
	store *Store,
	operation recordOperation,
	receipt PrepareReceipt,
	intent CommitIntent,
	pointer ActivePointer,
) error {
	switch operation {
	case recordPrepare:
		return store.PersistPrepareReceipt(receipt)
	case recordCommit:
		return store.PersistCommitIntent(intent)
	case recordActive:
		return store.PersistActivePointer(pointer)
	default:
		return ErrInvalidRecord
	}
}

func assertCrashOutcome(
	t *testing.T,
	store *Store,
	operation recordOperation,
	renamed bool,
	oldPointer ActivePointer,
	newReceipt PrepareReceipt,
	newIntent CommitIntent,
	newPointer ActivePointer,
) {
	t.Helper()
	switch operation {
	case recordPrepare:
		got, err := store.ReadPrepareReceipt(newReceipt.TransactionID)
		if !renamed && errors.Is(err, ErrRecordNotFound) {
			return
		}
		if err != nil || !renamed || !reflect.DeepEqual(got, newReceipt) {
			t.Fatalf("prepare crash outcome = %+v, %v, renamed=%v", got, err, renamed)
		}
	case recordCommit:
		got, err := store.ReadCommitIntent(newIntent.TransactionID)
		if !renamed && errors.Is(err, ErrRecordNotFound) {
			return
		}
		if err != nil || !renamed || !reflect.DeepEqual(got, newIntent) {
			t.Fatalf("commit crash outcome = %+v, %v, renamed=%v", got, err, renamed)
		}
	case recordActive:
		got, err := store.ReadActivePointer()
		want := oldPointer
		if renamed {
			want = newPointer
		}
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("active crash outcome = %+v, %v, want %+v", got, err, want)
		}
	}
}

func syntheticPrepareReceipt(t *testing.T, intent CommitIntent, domain policy.Domain) PrepareReceipt {
	t.Helper()
	payloadDigest := intent.RootPayloadSHA256
	policyGeneration := intent.RootPolicyGeneration
	if domain == policy.DomainUser {
		payloadDigest = intent.UserPayloadSHA256
		policyGeneration = intent.UserPolicyGeneration
	}
	receipt := PrepareReceipt{
		Schema: PrepareReceiptSchema, TransactionID: intent.TransactionID, Domain: domain,
		BundleGeneration: intent.BundleGeneration, PolicyGeneration: policyGeneration,
		ManifestSHA256: intent.ManifestSHA256, PayloadSHA256: payloadDigest,
		ApprovalSHA256: intent.ApprovalSHA256, PreparedAt: preparedAt,
	}
	if err := receipt.Validate(); err != nil {
		t.Fatalf("synthetic receipt: %v", err)
	}
	return receipt
}

func syntheticCommitIntent(t *testing.T, transactionID metadata.UUID, generation uint64) CommitIntent {
	t.Helper()
	manifestDigest := policy.SHA256Hex([]byte(fmt.Sprintf("synthetic-manifest-%d", generation)))
	rootDigest := policy.SHA256Hex([]byte(fmt.Sprintf("synthetic-root-%d", generation)))
	userDigest := policy.SHA256Hex([]byte(fmt.Sprintf("synthetic-user-%d", generation)))
	seed := bytes.Repeat([]byte{byte(generation)}, ed25519.SeedSize)
	privateKey := ed25519.NewKeyFromSeed(seed)
	publicKey := privateKey.Public().(ed25519.PublicKey)
	statement := policyapproval.ApprovalStatement{
		Schema: policyapproval.ApprovalSchema, ManifestSHA256: manifestDigest,
		RootSHA256: rootDigest, UserSHA256: userDigest,
		ReviewSHA256:      policy.SHA256Hex([]byte(fmt.Sprintf("synthetic-review-%d", generation))),
		SignerFingerprint: policy.SHA256Hex(publicKey),
		NotBefore:         "2030-01-01T00:00:00Z", ExpiresAt: "2030-01-01T01:00:00Z",
	}
	canonical, err := policy.MarshalCanonical(statement)
	if err != nil {
		t.Fatal(err)
	}
	approval := policyapproval.SignedApproval{
		Statement: statement,
		Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(privateKey, canonical)),
	}
	approvalDigest, err := approvalSHA256(approval)
	if err != nil {
		t.Fatal(err)
	}
	intent := CommitIntent{
		Schema: CommitIntentSchema, TransactionID: transactionID, BundleGeneration: generation,
		RootPolicyGeneration: generation, UserPolicyGeneration: generation,
		ManifestSHA256: manifestDigest, RootPayloadSHA256: rootDigest, UserPayloadSHA256: userDigest,
		ApprovalSHA256: approvalDigest, Approval: approval, CreatedAt: createdAt,
	}
	if err := intent.Validate(); err != nil {
		t.Fatalf("synthetic intent: %v", err)
	}
	return intent
}

func syntheticActivePointer(t *testing.T, intent CommitIntent, domain policy.Domain) ActivePointer {
	t.Helper()
	intentDigest, err := CommitIntentSHA256(intent)
	if err != nil {
		t.Fatal(err)
	}
	payloadDigest := intent.RootPayloadSHA256
	policyGeneration := intent.RootPolicyGeneration
	if domain == policy.DomainUser {
		payloadDigest = intent.UserPayloadSHA256
		policyGeneration = intent.UserPolicyGeneration
	}
	pointer := ActivePointer{
		Schema: ActivePointerSchema, TransactionID: intent.TransactionID, Domain: domain,
		BundleGeneration: intent.BundleGeneration, PolicyGeneration: policyGeneration,
		ManifestSHA256: intent.ManifestSHA256, PayloadSHA256: payloadDigest,
		ApprovalSHA256: intent.ApprovalSHA256, CommitIntentSHA256: intentDigest,
		Approval: intent.Approval, ActivatedAt: activatedAt,
	}
	if err := pointer.Validate(); err != nil {
		t.Fatalf("synthetic pointer: %v", err)
	}
	return pointer
}

func mustTransactionRecordFilename(
	t *testing.T,
	operation recordOperation,
	transactionID metadata.UUID,
) string {
	t.Helper()
	name, err := transactionRecordFilename(operation, transactionID)
	if err != nil {
		t.Fatal(err)
	}
	return name
}

func assertPrivateRecord(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm() != RecordFileMode || stat.Uid != currentUID() ||
		stat.Gid != currentGID() || stat.Nlink != 1 {
		t.Fatalf("record metadata is not private: mode=%v stat=%+v", info.Mode(), stat)
	}
}

func assertPrivateDirectory(t *testing.T, path string) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 ||
		info.Mode().Perm() != DirectoryMode || stat.Uid != currentUID() || stat.Gid != currentGID() {
		t.Fatalf("directory metadata is not private: mode=%v stat=%+v", info.Mode(), stat)
	}
}
