package signing

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

const (
	EnvelopeSchema  = "hexroute.signed-request.v1"
	EnvelopeVersion = 1
)

type Envelope struct {
	Schema     string        `json:"schema"`
	Version    uint16        `json:"version"`
	NodeID     metadata.UUID `json:"node_id"`
	KeyID      metadata.UUID `json:"key_id"`
	RequestID  metadata.UUID `json:"request_id"`
	Timestamp  string        `json:"timestamp"`
	BodySHA256 string        `json:"body_sha256"`
}

type SignedEnvelope struct {
	Envelope  Envelope `json:"envelope"`
	Signature string   `json:"signature"`
}

type KeyStatus string

const (
	KeyActive  KeyStatus = "active"
	KeyRetired KeyStatus = "retired"
	KeyRevoked KeyStatus = "revoked"
)

type RegisteredKey struct {
	NodeID    metadata.UUID
	KeyID     metadata.UUID
	PublicKey ed25519.PublicKey
	Status    KeyStatus
}

type Verifier struct {
	mu        sync.Mutex
	tolerance time.Duration
	keys      map[metadata.UUID]RegisteredKey
	seen      map[metadata.UUID]time.Time
}

var (
	ErrInvalidEnvelope  = errors.New("invalid signed request envelope")
	ErrInvalidSignature = errors.New("invalid request signature")
	ErrUnknownKey       = errors.New("unknown request signing key")
	ErrRevokedKey       = errors.New("request signing key is revoked")
	ErrReplay           = errors.New("request id was already used")
	ErrTimestamp        = errors.New("request timestamp is outside tolerance")
)

func Sign(
	key Key,
	requestID metadata.UUID,
	timestamp time.Time,
	body []byte,
) (SignedEnvelope, error) {
	if len(key.privateKey) != ed25519.PrivateKeySize {
		return SignedEnvelope{}, ErrInvalidKeyFile
	}
	if _, err := metadata.ParseUUID(string(requestID)); err != nil {
		return SignedEnvelope{}, ErrInvalidEnvelope
	}
	envelope := Envelope{
		Schema:     EnvelopeSchema,
		Version:    EnvelopeVersion,
		NodeID:     key.NodeID,
		KeyID:      key.KeyID,
		RequestID:  requestID,
		Timestamp:  timestamp.UTC().Format(time.RFC3339Nano),
		BodySHA256: bodyDigest(body),
	}
	canonical, err := canonicalEnvelope(envelope)
	if err != nil {
		return SignedEnvelope{}, err
	}
	signature := ed25519.Sign(key.privateKey, canonical)
	return SignedEnvelope{
		Envelope:  envelope,
		Signature: base64.RawURLEncoding.EncodeToString(signature),
	}, nil
}

func NewVerifier(tolerance time.Duration, keys []RegisteredKey) (*Verifier, error) {
	if tolerance <= 0 {
		return nil, ErrTimestamp
	}
	verifier := &Verifier{
		tolerance: tolerance,
		keys:      make(map[metadata.UUID]RegisteredKey, len(keys)),
		seen:      make(map[metadata.UUID]time.Time),
	}
	for _, key := range keys {
		if err := validateRegisteredKey(key); err != nil {
			return nil, err
		}
		if _, duplicate := verifier.keys[key.KeyID]; duplicate {
			return nil, ErrInvalidKeyFile
		}
		key.PublicKey = append(ed25519.PublicKey(nil), key.PublicKey...)
		verifier.keys[key.KeyID] = key
	}
	return verifier, nil
}

func (verifier *Verifier) Verify(
	signed SignedEnvelope,
	body []byte,
	now time.Time,
) error {
	canonical, timestamp, err := validateSignedEnvelope(signed, body)
	if err != nil {
		return err
	}
	if timestamp.Before(now.Add(-verifier.tolerance)) ||
		timestamp.After(now.Add(verifier.tolerance)) {
		return ErrTimestamp
	}

	verifier.mu.Lock()
	defer verifier.mu.Unlock()
	verifier.expireReplayWindow(now)

	registered, ok := verifier.keys[signed.Envelope.KeyID]
	if !ok || registered.NodeID != signed.Envelope.NodeID {
		return ErrUnknownKey
	}
	if registered.Status == KeyRevoked {
		return ErrRevokedKey
	}
	if registered.Status == KeyRetired {
		return ErrRevokedKey
	}
	if err := verifySignature(signed, canonical, registered); err != nil {
		return ErrInvalidSignature
	}
	if _, replayed := verifier.seen[signed.Envelope.RequestID]; replayed {
		return ErrReplay
	}
	verifier.seen[signed.Envelope.RequestID] = timestamp
	return nil
}

func VerifyAuthenticity(
	signed SignedEnvelope,
	body []byte,
	now time.Time,
	tolerance time.Duration,
	registered RegisteredKey,
) error {
	if tolerance <= 0 {
		return ErrTimestamp
	}
	canonical, timestamp, err := validateSignedEnvelope(signed, body)
	if err != nil {
		return err
	}
	if timestamp.Before(now.Add(-tolerance)) || timestamp.After(now.Add(tolerance)) {
		return ErrTimestamp
	}
	if err := validateRegisteredKey(registered); err != nil ||
		registered.KeyID != signed.Envelope.KeyID ||
		registered.NodeID != signed.Envelope.NodeID {
		return ErrUnknownKey
	}
	if registered.Status != KeyActive {
		return ErrRevokedKey
	}
	return verifySignature(signed, canonical, registered)
}

func (verifier *Verifier) Revoke(keyID metadata.UUID) error {
	verifier.mu.Lock()
	defer verifier.mu.Unlock()
	key, ok := verifier.keys[keyID]
	if !ok {
		return ErrUnknownKey
	}
	key.Status = KeyRevoked
	verifier.keys[keyID] = key
	return nil
}

func validateSignedEnvelope(
	signed SignedEnvelope,
	body []byte,
) ([]byte, time.Time, error) {
	envelope := signed.Envelope
	if envelope.Schema != EnvelopeSchema || envelope.Version != EnvelopeVersion ||
		envelope.BodySHA256 != bodyDigest(body) {
		return nil, time.Time{}, ErrInvalidEnvelope
	}
	for _, id := range []metadata.UUID{envelope.NodeID, envelope.KeyID, envelope.RequestID} {
		if _, err := metadata.ParseUUID(string(id)); err != nil {
			return nil, time.Time{}, ErrInvalidEnvelope
		}
	}
	timestamp, err := time.Parse(time.RFC3339Nano, envelope.Timestamp)
	if err != nil || timestamp.UTC().Format(time.RFC3339Nano) != envelope.Timestamp {
		return nil, time.Time{}, ErrInvalidEnvelope
	}
	if len(signed.Signature) > 128 {
		return nil, time.Time{}, ErrInvalidEnvelope
	}
	canonical, err := canonicalEnvelope(envelope)
	if err != nil {
		return nil, time.Time{}, err
	}
	return canonical, timestamp, nil
}

func canonicalEnvelope(envelope Envelope) ([]byte, error) {
	return json.Marshal(envelope)
}

func bodyDigest(body []byte) string {
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:])
}

func validateRegisteredKey(key RegisteredKey) error {
	for _, id := range []metadata.UUID{key.NodeID, key.KeyID} {
		if _, err := metadata.ParseUUID(string(id)); err != nil {
			return ErrInvalidKeyFile
		}
	}
	if len(key.PublicKey) != ed25519.PublicKeySize ||
		(key.Status != KeyActive && key.Status != KeyRetired && key.Status != KeyRevoked) {
		return ErrInvalidKeyFile
	}
	return nil
}

func verifySignature(
	signed SignedEnvelope,
	canonical []byte,
	registered RegisteredKey,
) error {
	signature, err := base64.RawURLEncoding.DecodeString(signed.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize ||
		!ed25519.Verify(registered.PublicKey, canonical, signature) {
		return ErrInvalidSignature
	}
	return nil
}

func (verifier *Verifier) expireReplayWindow(now time.Time) {
	cutoff := now.Add(-verifier.tolerance)
	for requestID, timestamp := range verifier.seen {
		if timestamp.Before(cutoff) {
			delete(verifier.seen, requestID)
		}
	}
}
