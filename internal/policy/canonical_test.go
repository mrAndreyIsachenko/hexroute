package policy

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalizePublishedRFC8785Vectors(t *testing.T) {
	for _, name := range []string{"values", "structures"} {
		t.Run(name, func(t *testing.T) {
			input, err := os.ReadFile(rfc8785Fixture(name + ".input.json"))
			if err != nil {
				t.Fatal(err)
			}
			expected, err := os.ReadFile(rfc8785Fixture(name + ".expected.json"))
			if err != nil {
				t.Fatal(err)
			}
			canonical, err := Canonicalize(input)
			if err != nil {
				t.Fatalf("canonicalize: %v", err)
			}
			expected = bytes.TrimSuffix(expected, []byte("\n"))
			if string(canonical) != string(expected) {
				t.Fatalf("canonical mismatch\nwant: %s\n got: %s", expected, canonical)
			}
		})
	}
}

func TestCanonicalSHA256IsDeterministic(t *testing.T) {
	left := map[string]any{
		"z": map[string]any{"value": 3.0, "enabled": true},
		"a": "first",
	}
	right := map[string]any{
		"a": "first",
		"z": map[string]any{"enabled": true, "value": 3},
	}
	leftDigest, leftCanonical, err := CanonicalSHA256(left)
	if err != nil {
		t.Fatal(err)
	}
	rightDigest, rightCanonical, err := CanonicalSHA256(right)
	if err != nil {
		t.Fatal(err)
	}
	if leftDigest != rightDigest || string(leftCanonical) != string(rightCanonical) {
		t.Fatalf("equivalent values must canonicalize identically: %s != %s", leftCanonical, rightCanonical)
	}
	if len(leftDigest) != 64 || leftDigest != SHA256Hex(leftCanonical) {
		t.Fatalf("unexpected canonical digest %q", leftDigest)
	}
}

func TestCanonicalizeRejectsInvalidOrOversizedInput(t *testing.T) {
	for _, input := range [][]byte{
		nil,
		[]byte(`{"unterminated":`),
		[]byte(strings.Repeat(" ", MaxCanonicalJSONSize+1)),
	} {
		if _, err := Canonicalize(input); !errors.Is(err, ErrInvalidCanonicalJSON) {
			t.Fatalf("invalid input should be rejected, got %v", err)
		}
	}
}

func rfc8785Fixture(name string) string {
	return filepath.Join("..", "..", "testdata", "policy", "rfc8785", name)
}
