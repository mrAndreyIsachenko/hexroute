package configpublish

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/configversion"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const testVersionID = metadata.UUID("11111111-1111-4111-8111-111111111111")

// Both fakes append to one list, so the order the publisher calls them in is
// itself observable.
type recordingStorage struct {
	calls *[]string
	keys  []string
	body  []byte
	err   error
}

func (storage *recordingStorage) PutVersion(_ context.Context, key string, content []byte) error {
	*storage.calls = append(*storage.calls, "store")
	storage.keys = append(storage.keys, key)
	storage.body = append([]byte(nil), content...)
	return storage.err
}

type recordingLedger struct {
	calls  *[]string
	record Record
	err    error
}

func (ledger *recordingLedger) RecordVersion(_ context.Context, record Record) error {
	*ledger.calls = append(*ledger.calls, "ledger")
	ledger.record = record
	return ledger.err
}

// A local key stands in for the Keychain here. What is under test is the
// publisher, and it holds no key at all: it verifies with a public one.
type localSigner struct{ private ed25519.PrivateKey }

func (signer localSigner) PublicKey() (ed25519.PublicKey, error) {
	return signer.private.Public().(ed25519.PublicKey), nil
}

func (signer localSigner) Sign(message []byte) ([]byte, error) {
	return ed25519.Sign(signer.private, message), nil
}

func signedVersion(t *testing.T, seed byte, content []byte) ([]byte, ed25519.PublicKey) {
	t.Helper()
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, ed25519.SeedSize))
	artifact, err := configversion.Publish(
		localSigner{private: private},
		configversion.Target{Kind: configversion.TargetNode, Key: "ingress-provider-b"},
		"2026-09-06.1",
		content,
		time.Date(2026, 9, 6, 10, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := configversion.Encode(artifact)
	if err != nil {
		t.Fatal(err)
	}
	return encoded, private.Public().(ed25519.PublicKey)
}

func publisherFor(t *testing.T, pinned ed25519.PublicKey) (*Publisher, *recordingStorage, *recordingLedger) {
	t.Helper()
	calls := &[]string{}
	storage := &recordingStorage{calls: calls}
	ledger := &recordingLedger{calls: calls}
	publisher, err := New(storage, ledger, pinned)
	if err != nil {
		t.Fatal(err)
	}
	return publisher, storage, ledger
}

func TestPublishRecordsThenStoresWhatTheOperatorSigned(t *testing.T) {
	content := []byte(`{"inbounds":[{"port":443}]}`)
	encoded, publicKey := signedVersion(t, 7, content)
	publisher, storage, ledger := publisherFor(t, publicKey)

	now := time.Date(2026, 9, 6, 11, 0, 0, 0, time.UTC)
	record, err := publisher.Publish(context.Background(), encoded, testVersionID, now)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}

	// The row decides whether the label may be used, so it is written first.
	// Reversed, an overwritten object would already be in the store when the
	// ledger refused the label.
	if strings.Join(*storage.calls, ",") != "ledger,store,store" {
		t.Fatalf("call order: %v", *storage.calls)
	}
	if !bytes.Equal(storage.body, encoded) {
		t.Fatal("the stored bytes are not the ones published")
	}
	// The labelled object is what the ledger names; the pointer is what a
	// host reads. Both are the same bytes, so a host cannot be handed one
	// version under the name of another.
	if strings.Join(storage.keys, ",") !=
		"versions/node/ingress-provider-b/2026-09-06.1.json,"+
			"versions/node/ingress-provider-b/current.json" ||
		record.ObjectKey != storage.keys[0] {
		t.Fatalf("object keys: %v", storage.keys)
	}
	if ledger.record.TargetKind != "node" || ledger.record.TargetKey != "ingress-provider-b" ||
		ledger.record.VersionLabel != "2026-09-06.1" ||
		ledger.record.SigningKeyID != SigningKeyID(publicKey) ||
		ledger.record.ConfigVersionID != testVersionID {
		t.Fatalf("ledger row: %+v", ledger.record)
	}
	if ledger.record.ContentSHA256 == ([32]byte{}) || ledger.record.Validate() != nil {
		t.Fatalf("ledger row does not validate: %+v", ledger.record)
	}
}

// The publisher holds no signing key. Refusing here is what stops the path
// with the store credential from becoming a second way to put bytes in front
// of a host.
func TestPublishRefusesAnythingItCannotVerify(t *testing.T) {
	content := []byte(`{"inbounds":[{"port":443}]}`)
	encoded, publicKey := signedVersion(t, 7, content)
	otherEncoded, _ := signedVersion(t, 9, content)

	for _, testCase := range []struct {
		name   string
		body   []byte
		pinned ed25519.PublicKey
	}{
		{name: "signed by another key", body: otherEncoded, pinned: publicKey},
		{name: "not a version at all", body: []byte(`{"a":1}`), pinned: publicKey},
		{name: "empty", body: nil, pinned: publicKey},
		{name: "tampered after signing", body: tamper(t, encoded), pinned: publicKey},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			publisher, storage, _ := publisherFor(t, testCase.pinned)
			record, err := publisher.Publish(
				context.Background(), testCase.body, testVersionID,
				time.Date(2026, 9, 6, 11, 0, 0, 0, time.UTC))
			if !errors.Is(err, ErrUnverified) {
				t.Fatalf("error: %v", err)
			}
			if record != (Record{}) || len(*storage.calls) != 0 {
				t.Fatalf("a refused version reached %v", *storage.calls)
			}
		})
	}
}

// A ledger that refused the row must leave the store untouched, or the object
// a host would fetch is already the new one while the record still names the
// old.
func TestARefusedRowLeavesTheStoreUntouched(t *testing.T) {
	encoded, publicKey := signedVersion(t, 7, []byte(`{"inbounds":[]}`))
	publisher, storage, ledger := publisherFor(t, publicKey)
	ledger.err = ErrLabelReused

	if _, err := publisher.Publish(context.Background(), encoded, testVersionID,
		time.Date(2026, 9, 6, 11, 0, 0, 0, time.UTC)); !errors.Is(err, ErrLabelReused) {
		t.Fatalf("error: %v", err)
	}
	if strings.Join(*storage.calls, ",") != "ledger" {
		t.Fatalf("calls: %v", *storage.calls)
	}
}

func TestAPublisherWithoutEveryDependencyIsRefused(t *testing.T) {
	_, publicKey := signedVersion(t, 7, []byte(`{}`))
	calls := &[]string{}
	storage := &recordingStorage{calls: calls}
	ledger := &recordingLedger{calls: calls}
	for _, testCase := range []struct {
		name    string
		storage Storage
		ledger  Ledger
		pinned  ed25519.PublicKey
	}{
		{name: "no storage", ledger: ledger, pinned: publicKey},
		{name: "no ledger", storage: storage, pinned: publicKey},
		{name: "no pinned key", storage: storage, ledger: ledger},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := New(testCase.storage, testCase.ledger, testCase.pinned); !errors.Is(err, ErrPublish) {
				t.Fatalf("accepted %s: %v", testCase.name, err)
			}
		})
	}
}

func tamper(t *testing.T, encoded []byte) []byte {
	t.Helper()
	artifact, err := configversion.Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	artifact.Statement.VersionLabel = "2026-09-06.2"
	tampered, err := configversion.Encode(artifact)
	if err != nil {
		t.Fatal(err)
	}
	return tampered
}
