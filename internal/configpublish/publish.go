// Package configpublish puts a signed configuration version where a host can
// fetch it, and records that it exists.
//
// It cannot sign. That is the point of it being a separate thing from the
// command that can: publishing needs a store credential and a database, and
// neither of those should sit next to the operator key.
package configpublish

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/configversion"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

// KeyPrefix is where versions live in the bucket. A host fetches under this
// prefix and nothing else, so a credential scoped to it can read versions and
// nothing else either.
const KeyPrefix = "versions"

var (
	// ErrPublish is any failure to publish.
	ErrPublish = errors.New("configuration version was not published")
	// ErrUnverified is an artifact this publisher would not vouch for. It is
	// separate because the operator's response differs: sign it again, rather
	// than retry.
	ErrUnverified = errors.New("configuration version did not verify before publishing")
)

// Storage is the private bucket, in publish access.
type Storage interface {
	PutVersion(ctx context.Context, key string, content []byte) error
}

// Ledger records what exists. It is a record and not an instruction: nothing
// reads a row of it to decide that a host should change.
type Ledger interface {
	RecordVersion(ctx context.Context, record Record) error
}

// Record is one row of the configuration ledger.
type Record struct {
	ConfigVersionID metadata.UUID
	TargetKind      string
	TargetKey       string
	SchemaVersion   int
	VersionLabel    string
	ContentSHA256   [32]byte
	SigningKeyID    string
	ObjectKey       string
	CreatedAt       time.Time
}

// Publisher stores and records one signed version at a time.
type Publisher struct {
	storage Storage
	ledger  Ledger
	// pinned is the operator public key. The publisher verifies against it so
	// that a component which cannot sign also cannot publish something
	// unsigned by reaching the store directly through this path.
	pinned ed25519.PublicKey
}

// New builds a publisher. Every dependency is required: a publisher that
// defaulted its store or its ledger would publish somewhere nobody chose.
func New(storage Storage, ledger Ledger, pinned ed25519.PublicKey) (*Publisher, error) {
	if storage == nil || ledger == nil || len(pinned) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("%w: incomplete publisher", ErrPublish)
	}
	return &Publisher{
		storage: storage,
		ledger:  ledger,
		pinned:  append(ed25519.PublicKey(nil), pinned...),
	}, nil
}

// ObjectKey is where one version lives. It is derived from what the operator
// signed, so a retry after a failure writes the same object rather than a
// second one.
func ObjectKey(statement configversion.Statement) string {
	return prefixFor(statement.TargetKind, statement.TargetKey) +
		statement.VersionLabel + ".json"
}

// CurrentKey is the one key a host reads. It is how a node learns a version
// exists without the cloud telling it: the node asks, on its own schedule, and
// what it finds there is either a version it can verify or nothing it will
// act on.
//
// The pointer is not an instruction. It carries the same signed statement as
// the labelled object, so a host that read it still decides for itself.
func CurrentKey(kind string, key string) string {
	return prefixFor(kind, key) + "current.json"
}

func prefixFor(kind string, key string) string {
	return KeyPrefix + "/" + kind + "/" + key + "/"
}

// Publish verifies, records and stores one version.
//
// The row is written before the object, because the row is what decides
// whether this label may be used at all: a label already naming different
// content is refused there, before anything in the store is overwritten. A row
// whose object never landed names a version no host can fetch, which leaves
// every host on what it is already running; publishing again completes it.
func (publisher *Publisher) Publish(
	ctx context.Context,
	encoded []byte,
	versionID metadata.UUID,
	now time.Time,
) (Record, error) {
	if publisher == nil || ctx == nil {
		return Record{}, fmt.Errorf("%w: no publisher", ErrPublish)
	}
	if _, err := metadata.ParseUUID(string(versionID)); err != nil || now.IsZero() {
		return Record{}, fmt.Errorf("%w: invalid publication", ErrPublish)
	}
	artifact, err := configversion.Decode(encoded)
	if err != nil {
		return Record{}, fmt.Errorf("%w: %w", ErrUnverified, err)
	}
	statement := artifact.Statement
	target := configversion.Target{
		Kind: configversion.TargetKind(statement.TargetKind),
		Key:  statement.TargetKey,
	}
	if _, err := configversion.Verify(artifact, publisher.pinned, target); err != nil {
		return Record{}, fmt.Errorf("%w: %w", ErrUnverified, err)
	}

	digest, err := hex.DecodeString(statement.ContentSHA256)
	if err != nil || len(digest) != 32 {
		return Record{}, fmt.Errorf("%w: content digest", ErrUnverified)
	}
	record := Record{
		ConfigVersionID: versionID,
		TargetKind:      statement.TargetKind,
		TargetKey:       statement.TargetKey,
		SchemaVersion:   int(statement.Version),
		VersionLabel:    statement.VersionLabel,
		SigningKeyID:    statement.SignerFingerprint,
		ObjectKey:       ObjectKey(statement),
		CreatedAt:       now.UTC(),
	}
	copy(record.ContentSHA256[:], digest)

	if err := publisher.ledger.RecordVersion(ctx, record); err != nil {
		return Record{}, fmt.Errorf("%w: %w", ErrPublish, err)
	}
	if err := publisher.storage.PutVersion(ctx, record.ObjectKey, encoded); err != nil {
		return Record{}, fmt.Errorf("%w: %w", ErrPublish, err)
	}
	// The labelled object is written first, so the key a host reads never
	// names bytes that are not already stored under their own name.
	if err := publisher.storage.PutVersion(
		ctx, CurrentKey(statement.TargetKind, statement.TargetKey), encoded,
	); err != nil {
		return Record{}, fmt.Errorf("%w: %w", ErrPublish, err)
	}
	return record, nil
}

// Validate is what the ledger requires of a row before it writes one.
func (record Record) Validate() error {
	if _, err := metadata.ParseUUID(string(record.ConfigVersionID)); err != nil {
		return fmt.Errorf("%w: version id", ErrPublish)
	}
	switch configversion.TargetKind(record.TargetKind) {
	case configversion.TargetNode, configversion.TargetGroup, configversion.TargetGlobal:
	default:
		return fmt.Errorf("%w: target kind", ErrPublish)
	}
	if record.TargetKey == "" || len(record.TargetKey) > 192 ||
		record.VersionLabel == "" || len(record.VersionLabel) > 128 ||
		record.SchemaVersion <= 0 || record.ObjectKey == "" ||
		len(record.SigningKeyID) != 64 || record.CreatedAt.IsZero() {
		return fmt.Errorf("%w: ledger row", ErrPublish)
	}
	if record.ContentSHA256 == ([32]byte{}) {
		return fmt.Errorf("%w: content digest", ErrPublish)
	}
	return nil
}

// SigningKeyID names a public key the way a ledger row does.
func SigningKeyID(publicKey ed25519.PublicKey) string {
	return policy.SHA256Hex(publicKey)
}
