package pritunlclient

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/credentials"
)

const (
	testPIN  = "8461"
	testSeed = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
)

// keychainRunner answers the two Keychain reads the credential source makes.
type keychainRunner struct{ err error }

func (runner keychainRunner) Output(
	_ context.Context,
	_ string,
	args ...string,
) ([]byte, error) {
	if runner.err != nil {
		return nil, runner.err
	}
	for index, argument := range args {
		if argument == "-s" && index+1 < len(args) {
			if strings.Contains(args[index+1], "totp") {
				return []byte(testSeed + "\n"), nil
			}
			return []byte(testPIN + "\n"), nil
		}
	}
	return nil, errors.New("no service named")
}

// recordingRunner keeps everything the client was asked to run, so a test can
// look at exactly what would have reached the process table.
type recordingRunner struct {
	input []byte
	name  string
	args  []string
	calls int
	err   error
}

func (runner *recordingRunner) RunWithInput(
	_ context.Context,
	input []byte,
	name string,
	args ...string,
) error {
	runner.calls++
	runner.input = append([]byte(nil), input...)
	runner.name = name
	runner.args = append([]string(nil), args...)
	return runner.err
}

func testSource(t *testing.T, runner keychainRunner) credentials.Source {
	t.Helper()
	source, err := credentials.NewKeychainSource(runner, credentials.KeychainConfig{
		Account: "operator", PINService: "hexroute-pritunl-pin",
		TOTPService: "hexroute-pritunl-totp",
	})
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func testClient(t *testing.T, runner *recordingRunner, at time.Time) *Client {
	t.Helper()
	client, err := New(Config{
		Path:      "/Applications/Example.app/Contents/Resources/example-client",
		ProfileID: "0123456789abcdef",
		Mode:      "wg",
	}, runner)
	if err != nil {
		t.Fatal(err)
	}
	client.now = func() time.Time { return at }
	return client
}

// The whole point of this package: the secret goes to the client's input, and
// nowhere a process listing, an environment or a log could reach it.
func TestTheSecretReachesTheInputAndNothingElse(t *testing.T) {
	runner := &recordingRunner{}
	client := testClient(t, runner, time.Unix(1111111110, 0).UTC())
	if err := client.Reconnect(context.Background(), testSource(t, keychainRunner{})); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	if runner.calls != 1 {
		t.Fatalf("calls = %d", runner.calls)
	}

	submitted := string(runner.input)
	if !strings.HasPrefix(submitted, testPIN) || len(submitted) != len(testPIN)+6 {
		t.Fatalf("submission is not the PIN followed by a six digit code: %d bytes", len(submitted))
	}
	code := submitted[len(testPIN):]

	// Nothing of what was submitted appears in an argument.
	joined := strings.Join(runner.args, " ")
	for _, secret := range []string{testPIN, testSeed, code, submitted} {
		if strings.Contains(joined, secret) || strings.Contains(runner.name, secret) {
			t.Fatalf("an argument carries a secret: %q", joined)
		}
	}
	// And the client is asked for its password-read path rather than a flag.
	if !strings.Contains(joined, "-r") || strings.Contains(joined, "-p") {
		t.Fatalf("args = %q", joined)
	}
	if joined != "start 0123456789abcdef -m wg -r" {
		t.Fatalf("args = %q", joined)
	}
}

// A failure is reported as a failure. Nothing about what was submitted, and
// nothing the client wrote back, becomes part of it.
func TestAFailureSaysNothingAboutWhatWasSubmitted(t *testing.T) {
	runner := &recordingRunner{err: errors.New("exit status 1: password " + testPIN + " rejected")}
	client := testClient(t, runner, time.Unix(1111111110, 0).UTC())
	err := client.Reconnect(context.Background(), testSource(t, keychainRunner{}))
	if !errors.Is(err, ErrReconnect) {
		t.Fatalf("error = %v", err)
	}
	for _, secret := range []string{testPIN, testSeed} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("the error repeats a secret: %v", err)
		}
	}

	// Credentials that cannot be read are also a failure that says only that.
	unreadable := testSource(t, keychainRunner{err: errors.New("keychain: " + testPIN)})
	err = client.Reconnect(context.Background(), unreadable)
	if !errors.Is(err, ErrReconnect) || strings.Contains(err.Error(), testPIN) {
		t.Fatalf("error = %v", err)
	}
	if runner.calls != 1 {
		t.Fatal("the client was run without a credential")
	}
}

// A code with too little life left is not submitted. Submitting one fails in a
// way indistinguishable from a wrong secret, which is the worst diagnosis this
// path can produce.
func TestACodeAboutToExpireIsNotSubmitted(t *testing.T) {
	runner := &recordingRunner{}
	// One second before the window closes.
	client := testClient(t, runner, time.Unix(1111111110-1111111110%30+29, 0).UTC())
	err := client.Reconnect(context.Background(), testSource(t, keychainRunner{}))
	if !errors.Is(err, ErrWindowTooShort) || runner.calls != 0 {
		t.Fatalf("error = %v, calls = %d", err, runner.calls)
	}
}

func TestAClientNeedsAPathAProfileAndAMode(t *testing.T) {
	runner := &recordingRunner{}
	for _, testCase := range []struct {
		name   string
		config Config
	}{
		{name: "no path", config: Config{ProfileID: "0123456789abcdef", Mode: "wg"}},
		{name: "relative path", config: Config{Path: "client", ProfileID: "0123456789abcdef", Mode: "wg"}},
		{name: "no profile", config: Config{Path: "/usr/local/bin/client", Mode: "wg"}},
		{name: "a profile that is not one", config: Config{
			Path: "/usr/local/bin/client", ProfileID: "../other", Mode: "wg",
		}},
		{name: "an unknown mode", config: Config{
			Path: "/usr/local/bin/client", ProfileID: "0123456789abcdef", Mode: "ipsec",
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := New(testCase.config, runner); !errors.Is(err, ErrInvalidClient) {
				t.Fatalf("accepted %s", testCase.name)
			}
		})
	}
	if _, err := New(Config{
		Path: "/usr/local/bin/client", ProfileID: "0123456789abcdef", Mode: "wg",
	}, nil); !errors.Is(err, ErrInvalidClient) {
		t.Fatal("accepted a client with no runner")
	}
}
