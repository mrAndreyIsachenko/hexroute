package pritunlclient

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A reconnect that fails says where it stopped.
//
// It passes through several steps that fail for unrelated reasons, and every
// one of them arrived at the log as the single word "failed". On 2026-09-09 an
// attempt failed and two explanations fitted — credentials an unattended agent
// cannot read, and a client refusing to connect while Pritunl was already
// reconnecting the session three seconds later. Choosing between them meant
// reading source, and source cannot say which one happened.
//
// The steps send the reader to different places: to the Keychain, to the
// client, to the code itself. That is why they are separate.
func TestAReconnectSaysWhichStepItStoppedAt(t *testing.T) {
	at := time.Unix(1111111110, 0).UTC()
	for _, testCase := range []struct {
		name     string
		keychain keychainRunner
		client   *recordingRunner
		want     error
	}{
		{
			name:     "the credentials could not be read",
			keychain: keychainRunner{err: errors.New("the item is not readable here")},
			client:   &recordingRunner{},
			want:     ErrCredentialsUnavailable,
		},
		{
			name:     "the client would not start the session",
			keychain: keychainRunner{},
			client:   &recordingRunner{err: errors.New("a connect is already under way")},
			want:     ErrSessionNotStarted,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			client := testClient(t, testCase.client, at)
			err := client.Reconnect(
				context.Background(), testSource(t, testCase.keychain))
			if !errors.Is(err, testCase.want) {
				t.Fatalf("Reconnect() = %v, want %v", err, testCase.want)
			}
		})
	}
}

// Naming them must not lose what they have in common.
//
// A caller that only needs to know the attempt failed keeps working, and one
// that needs to know where can still tell them apart from each other.
func TestANamedFailureIsStillAReconnectFailure(t *testing.T) {
	for _, err := range []error{ErrCredentialsUnavailable, ErrSessionNotStarted} {
		if !errors.Is(err, ErrReconnect) {
			t.Fatalf("%v is not a reconnect failure", err)
		}
	}
	if errors.Is(ErrCredentialsUnavailable, ErrSessionNotStarted) {
		t.Fatal("the two are indistinguishable from each other")
	}
	// And the window is its own thing, as it already was: a code about to
	// expire is a reason to wait rather than an attempt that did not work.
	if errors.Is(ErrWindowTooShort, ErrReconnect) {
		t.Fatal("a window too short reads as a failed attempt")
	}
}

// A named failure still says nothing about what it was carrying.
func TestANamedFailureCarriesNoSecret(t *testing.T) {
	client := testClient(t, &recordingRunner{
		err: errors.New("the client refused"),
	}, time.Unix(1111111110, 0).UTC())
	err := client.Reconnect(context.Background(), testSource(t, keychainRunner{}))
	if err == nil {
		t.Fatal("expected a failure")
	}
	for _, secret := range []string{testPIN, testSeed} {
		if contains(err.Error(), secret) {
			t.Fatalf("the failure carries what was submitted: %v", err)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) &&
		func() bool {
			for i := 0; i+len(needle) <= len(haystack); i++ {
				if haystack[i:i+len(needle)] == needle {
					return true
				}
			}
			return false
		}()
}
