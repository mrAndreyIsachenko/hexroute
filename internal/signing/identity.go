package signing

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

// identityFileSchema names the public half of a node key on disk.
const identityFileSchema = "hexroute.node-identity.v1"

// PublicIdentity is who a node is, without being able to speak as it.
//
// It exists so that something on the same host as an observer, or anywhere
// else, can verify what that observer signed without holding the key that
// signs. The key file cannot be used for this: reading it would put the
// private key in a process that has no business signing anything.
type PublicIdentity struct {
	Schema    string        `json:"schema"`
	NodeID    metadata.UUID `json:"node_id"`
	KeyID     metadata.UUID `json:"key_id"`
	PublicKey string        `json:"public_key"`
}

// Identity is the public identity of this key, in the form it is published.
func (key Key) Identity() (PublicIdentity, error) {
	publicKey := key.PublicKey()
	if len(publicKey) != ed25519.PublicKeySize {
		return PublicIdentity{}, ErrInvalidKeyFile
	}
	return PublicIdentity{
		Schema:    identityFileSchema,
		NodeID:    key.NodeID,
		KeyID:     key.KeyID,
		PublicKey: base64.RawStdEncoding.EncodeToString(publicKey),
	}, nil
}

// LoadPublicIdentityFile reads a node's public identity.
//
// Unlike a key file, this one is deliberately readable: it carries nothing
// that could be used to sign, and the whole point of it is that verification
// does not require the authority to produce what it verifies.
func LoadPublicIdentityFile(path string) (RegisteredKey, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return RegisteredKey{}, err
	}
	if !info.Mode().IsRegular() {
		return RegisteredKey{}, ErrInvalidKeyFile
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		return RegisteredKey{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var stored PublicIdentity
	if err := decoder.Decode(&stored); err != nil {
		return RegisteredKey{}, ErrInvalidKeyFile
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return RegisteredKey{}, ErrInvalidKeyFile
	}
	if stored.Schema != identityFileSchema {
		return RegisteredKey{}, ErrInvalidKeyFile
	}
	publicKey, err := base64.RawStdEncoding.DecodeString(stored.PublicKey)
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return RegisteredKey{}, ErrInvalidKeyFile
	}
	registered := RegisteredKey{
		NodeID:    stored.NodeID,
		KeyID:     stored.KeyID,
		PublicKey: ed25519.PublicKey(publicKey),
		Status:    KeyActive,
	}
	if err := validateRegisteredKey(registered); err != nil {
		return RegisteredKey{}, err
	}
	return registered, nil
}
