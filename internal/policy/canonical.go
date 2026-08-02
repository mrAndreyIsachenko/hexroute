package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/cyberphone/json-canonicalization/go/src/webpki.org/jsoncanonicalizer"
)

const MaxCanonicalJSONSize = 1024 * 1024

var ErrInvalidCanonicalJSON = errors.New("invalid canonical JSON input")

func MarshalCanonical(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, ErrInvalidCanonicalJSON
	}
	return Canonicalize(encoded)
}

func Canonicalize(encoded []byte) ([]byte, error) {
	if len(encoded) == 0 || len(encoded) > MaxCanonicalJSONSize {
		return nil, ErrInvalidCanonicalJSON
	}
	canonical, err := jsoncanonicalizer.Transform(encoded)
	if err != nil || len(canonical) == 0 || len(canonical) > MaxCanonicalJSONSize {
		return nil, ErrInvalidCanonicalJSON
	}
	return canonical, nil
}

func CanonicalSHA256(value any) (string, []byte, error) {
	canonical, err := MarshalCanonical(value)
	if err != nil {
		return "", nil, err
	}
	return SHA256Hex(canonical), canonical, nil
}

func SHA256Hex(content []byte) string {
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}
