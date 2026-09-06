// Package otp derives time-based one-time codes.
//
// It exists because recovering a Pritunl session needs one and nothing here
// produced one. The legacy path shelled out to a separate tool and then put the
// result in a command-line argument; this produces the code in process and
// hands it to a caller that never lets it become an argument.
package otp

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// Period is the window a code is valid for. It is not configurable: the
	// authenticator it must agree with is not configurable either.
	Period = 30 * time.Second
	// Digits is the length of a code.
	Digits = 6
	// maxSeedBytes bounds a decoded seed. A shared secret this size is already
	// far beyond anything an authenticator issues.
	maxSeedBytes = 128
)

// ErrInvalidSeed is a seed that is not a usable shared secret. It never
// contains the seed, or any part of it.
var ErrInvalidSeed = errors.New("invalid one-time-code seed")

// DecodeSeed reads a base32 shared secret as an authenticator writes it:
// unpadded or padded, in either case, and with the spaces people put in when
// they copy one by hand.
//
// The caller owns the returned bytes and should clear them.
func DecodeSeed(encoded []byte) ([]byte, error) {
	normalized := strings.ToUpper(strings.Join(strings.Fields(string(encoded)), ""))
	if normalized == "" || len(normalized) > 4*maxSeedBytes {
		return nil, ErrInvalidSeed
	}
	for _, encoding := range []*base32.Encoding{
		base32.StdEncoding.WithPadding(base32.NoPadding),
		base32.StdEncoding,
	} {
		decoded, err := encoding.DecodeString(strings.TrimRight(normalized, "="))
		if err == nil && len(decoded) > 0 && len(decoded) <= maxSeedBytes {
			return decoded, nil
		}
		clear(decoded)
	}
	return nil, ErrInvalidSeed
}

// Code derives the code for a moment.
//
// The seed is read and not retained. Errors name the shape of the failure and
// never the secret, because an error string is the one part of a failure that
// reliably reaches a log.
func Code(seed []byte, at time.Time) (string, error) {
	return code(seed, at, Period, Digits)
}

// SecondsRemaining is how long the code for a moment stays valid.
//
// A caller with too little left should wait rather than submit: a code that
// expires between being read and being checked fails in a way that looks like a
// wrong secret.
func SecondsRemaining(at time.Time, period time.Duration) (uint32, error) {
	if at.IsZero() || period <= 0 {
		return 0, ErrInvalidSeed
	}
	seconds := int64(period / time.Second)
	if seconds <= 0 {
		return 0, ErrInvalidSeed
	}
	return uint32(seconds - at.UTC().Unix()%seconds), nil
}

func code(seed []byte, at time.Time, period time.Duration, digits int) (string, error) {
	if len(seed) == 0 || len(seed) > maxSeedBytes || at.IsZero() ||
		period <= 0 || digits < 6 || digits > 10 {
		return "", ErrInvalidSeed
	}
	seconds := int64(period / time.Second)
	if seconds <= 0 {
		return "", ErrInvalidSeed
	}
	counter := at.UTC().Unix() / seconds
	if counter < 0 {
		return "", ErrInvalidSeed
	}

	var message [8]byte
	binary.BigEndian.PutUint64(message[:], uint64(counter))
	mac := hmac.New(sha1.New, seed)
	if _, err := mac.Write(message[:]); err != nil {
		return "", ErrInvalidSeed
	}
	sum := mac.Sum(nil)
	defer clear(sum)

	offset := sum[len(sum)-1] & 0x0f
	truncated := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	modulus := uint32(1)
	for index := 0; index < digits; index++ {
		modulus *= 10
	}
	return fmt.Sprintf("%0*d", digits, truncated%modulus), nil
}
