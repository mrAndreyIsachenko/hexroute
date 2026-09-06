package otp

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

// The published RFC 6238 vectors, SHA-1. They are the reason to believe this
// agrees with the authenticator it has to agree with: a code derivation that
// is merely self-consistent produces six digits nobody else accepts.
//
// The vectors are eight digits; production uses six. Digits are the final
// truncation, so a derivation that matches at eight matches at six.
func TestPublishedVectorsAreReproduced(t *testing.T) {
	seed := []byte("12345678901234567890")
	for _, vector := range []struct {
		unix int64
		want string
	}{
		{unix: 59, want: "94287082"},
		{unix: 1111111109, want: "07081804"},
		{unix: 1111111111, want: "14050471"},
		{unix: 1234567890, want: "89005924"},
		{unix: 2000000000, want: "69279037"},
		{unix: 20000000000, want: "65353130"},
	} {
		got, err := code(seed, time.Unix(vector.unix, 0).UTC(), Period, 8)
		if err != nil || got != vector.want {
			t.Fatalf("T=%d: code = %q (%v), want %q", vector.unix, got, err, vector.want)
		}
	}
}

// A code is derived from the moment, and every moment inside one window gives
// the same one. That is what makes waiting for the next window meaningful.
func TestOneWindowGivesOneCode(t *testing.T) {
	seed := []byte("12345678901234567890")
	base := time.Unix(1111111110, 0).UTC()
	first, err := Code(seed, base)
	if err != nil || len(first) != Digits {
		t.Fatalf("code = %q, %v", first, err)
	}
	for _, offset := range []time.Duration{0, time.Second, 19 * time.Second} {
		again, err := Code(seed, base.Add(offset))
		if err != nil || again != first {
			t.Fatalf("offset %s: code = %q, want %q", offset, again, first)
		}
	}
	next, err := Code(seed, base.Add(Period))
	if err != nil || next == first {
		t.Fatalf("the next window repeated the code: %q", next)
	}

	remaining, err := SecondsRemaining(base, Period)
	if err != nil || remaining == 0 || remaining > uint32(Period/time.Second) {
		t.Fatalf("remaining = %d, %v", remaining, err)
	}
	boundary, err := SecondsRemaining(time.Unix(1111111110-1111111110%30, 0).UTC(), Period)
	if err != nil || boundary != uint32(Period/time.Second) {
		t.Fatalf("boundary remaining = %d, %v", boundary, err)
	}
}

// A seed arrives as an authenticator writes it, and people copy them by hand.
func TestSeedsAreReadAsAuthenticatorsWriteThem(t *testing.T) {
	expected := []byte("12345678901234567890")
	for _, encoded := range []string{
		"GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ",
		"gezdgnbvgy3tqojqgezdgnbvgy3tqojq",
		"GEZD GNBV GY3T QOJQ GEZD GNBV GY3T QOJQ",
	} {
		decoded, err := DecodeSeed([]byte(encoded))
		if err != nil || !bytes.Equal(decoded, expected) {
			t.Fatalf("%q decoded to %q (%v)", encoded, decoded, err)
		}
	}
}

// Everything a failure says has to be safe to log, because an error string is
// the part of a failure that reliably reaches a log.
func TestFailuresNameTheShapeAndNotTheSecret(t *testing.T) {
	secret := "GEZDGNBVGY3TQOJQ"
	for _, testCase := range []struct {
		name    string
		encoded string
	}{
		{name: "empty", encoded: ""},
		{name: "not base32", encoded: secret + "!!!"},
		{name: "longer than any secret", encoded: strings.Repeat("A", 4*maxSeedBytes+8)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			decoded, err := DecodeSeed([]byte(testCase.encoded))
			if !errors.Is(err, ErrInvalidSeed) || decoded != nil {
				t.Fatalf("decoded = %q, err = %v", decoded, err)
			}
			if strings.Contains(err.Error(), secret) ||
				(testCase.encoded != "" && strings.Contains(err.Error(), testCase.encoded)) {
				t.Fatalf("the error repeats its input: %v", err)
			}
		})
	}

	if _, err := Code(nil, time.Unix(59, 0).UTC()); !errors.Is(err, ErrInvalidSeed) {
		t.Fatalf("empty seed: %v", err)
	}
	if _, err := Code([]byte("12345678901234567890"), time.Time{}); !errors.Is(err, ErrInvalidSeed) {
		t.Fatalf("zero time: %v", err)
	}
}
