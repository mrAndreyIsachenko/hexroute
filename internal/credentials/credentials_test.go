package credentials

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"testing"

	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
)

type runnerCall struct {
	name string
	args []string
}

type fakeRunner struct {
	outputs map[string][]byte
	errors  map[string]error
	calls   []runnerCall
}

func (runner *fakeRunner) Output(
	_ context.Context,
	name string,
	args ...string,
) ([]byte, error) {
	key := fakeKey(name, args...)
	runner.calls = append(runner.calls, runnerCall{
		name: name,
		args: append([]string(nil), args...),
	})
	if err := runner.errors[key]; err != nil {
		return nil, err
	}
	return runner.outputs[key], nil
}

func fakeKey(name string, args ...string) string {
	result := name
	for _, arg := range args {
		result += "\x00" + arg
	}
	return result
}

func testConfig() KeychainConfig {
	return KeychainConfig{
		Account:     "unit-user",
		PINService:  "hexroute-pritunl-pin",
		TOTPService: "hexroute-pritunl-otp",
	}
}

func keychainArgs(service string) []string {
	return []string{
		"find-generic-password",
		"-s",
		service,
		"-a",
		testConfig().Account,
		"-w",
	}
}

func TestKeychainSourceKeepsSecretsOutOfArgumentsAndFormatting(t *testing.T) {
	seedValue := []byte("seed-unit-value\n")
	pinValue := []byte("pin-unit-value\n")
	runner := &fakeRunner{outputs: map[string][]byte{
		fakeKey(securityCommand, keychainArgs(testConfig().TOTPService)...): seedValue,
		fakeKey(securityCommand, keychainArgs(testConfig().PINService)...):  pinValue,
	}}
	source, err := NewKeychainSource(runner, testConfig())
	if err != nil {
		t.Fatalf("NewKeychainSource() error: %v", err)
	}

	credentials, err := source.ReadPritunl(context.Background())
	if err != nil {
		t.Fatalf("ReadPritunl() error: %v", err)
	}
	defer credentials.Close()

	var gotPIN, gotSeed []byte
	if err := credentials.UsePIN(func(value []byte) error {
		gotPIN = append([]byte(nil), value...)
		return nil
	}); err != nil {
		t.Fatalf("UsePIN() error: %v", err)
	}
	if err := credentials.UseTOTPSeed(func(value []byte) error {
		gotSeed = append([]byte(nil), value...)
		return nil
	}); err != nil {
		t.Fatalf("UseTOTPSeed() error: %v", err)
	}
	if string(gotPIN) != "pin-unit-value" || string(gotSeed) != "seed-unit-value" {
		t.Fatal("credential callback received unexpected values")
	}
	clear(gotPIN)
	clear(gotSeed)

	wantCalls := []runnerCall{
		{name: securityCommand, args: keychainArgs(testConfig().TOTPService)},
		{name: securityCommand, args: keychainArgs(testConfig().PINService)},
	}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, wantCalls)
	}
	for _, call := range runner.calls {
		joined := fmt.Sprint(call.args)
		if bytes.Contains([]byte(joined), []byte("seed-unit-value")) ||
			bytes.Contains([]byte(joined), []byte("pin-unit-value")) {
			t.Fatal("secret reached command arguments")
		}
	}

	formatted := []string{
		fmt.Sprint(credentials),
		fmt.Sprintf("%+v", credentials),
		fmt.Sprintf("%#v", credentials),
		fmt.Sprint(*credentials),
		fmt.Sprintf("%+v", *credentials),
		fmt.Sprintf("%#v", *credentials),
		slog.AnyValue(credentials).Resolve().String(),
		slog.AnyValue(*credentials).Resolve().String(),
	}
	for _, value := range formatted {
		if value != "[REDACTED]" {
			t.Fatalf("formatted credential = %q", value)
		}
	}
	if _, err := json.Marshal(credentials); !errors.Is(err, ErrSecretSerialization) {
		t.Fatalf("json.Marshal() error = %v, want %v", err, ErrSecretSerialization)
	}
	if _, err := json.Marshal(*credentials); !errors.Is(err, ErrSecretSerialization) {
		t.Fatalf("json.Marshal(value) error = %v, want %v", err, ErrSecretSerialization)
	}
	if _, err := credentials.MarshalText(); !errors.Is(err, ErrSecretSerialization) {
		t.Fatalf("MarshalText() error = %v, want %v", err, ErrSecretSerialization)
	}
	if _, err := credentials.MarshalBinary(); !errors.Is(err, ErrSecretSerialization) {
		t.Fatalf("MarshalBinary() error = %v, want %v", err, ErrSecretSerialization)
	}
}

func TestCredentialUseClearsTemporaryCopyAndCloseClearsStoredValues(t *testing.T) {
	pin := []byte("pin-unit-value")
	seed := []byte("seed-unit-value")
	credentials, err := newPritunl(pin, seed)
	if err != nil {
		t.Fatalf("newPritunl() error: %v", err)
	}

	var callbackValue []byte
	if err := credentials.UsePIN(func(value []byte) error {
		callbackValue = value
		return nil
	}); err != nil {
		t.Fatalf("UsePIN() error: %v", err)
	}
	if !allZero(callbackValue) {
		t.Fatal("temporary callback copy was not cleared")
	}

	pinBacking := credentials.pin.value
	seedBacking := credentials.totpSeed.value
	if err := credentials.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
	if !allZero(pinBacking) || !allZero(seedBacking) {
		t.Fatal("stored credential buffers were not cleared")
	}
	if err := credentials.UsePIN(func([]byte) error { return nil }); !errors.Is(
		err,
		ErrCredentialsClosed,
	) {
		t.Fatalf("UsePIN() after Close error = %v, want %v", err, ErrCredentialsClosed)
	}
}

func TestKeychainFailureDoesNotReturnPartialCredentials(t *testing.T) {
	seedValue := []byte("seed-unit-value\n")
	pinErr := errors.New("Keychain item unavailable")
	runner := &fakeRunner{
		outputs: map[string][]byte{
			fakeKey(securityCommand, keychainArgs(testConfig().TOTPService)...): seedValue,
		},
		errors: map[string]error{
			fakeKey(securityCommand, keychainArgs(testConfig().PINService)...): pinErr,
		},
	}
	source, _ := NewKeychainSource(runner, testConfig())

	credentials, err := source.ReadPritunl(context.Background())
	if !errors.Is(err, pinErr) || credentials != nil {
		t.Fatalf("ReadPritunl() = %v, %v", credentials, err)
	}
	if !allZero(seedValue) {
		t.Fatal("Keychain command output was not cleared after partial failure")
	}
}

func TestKeychainConfigurationAndSecretBounds(t *testing.T) {
	if _, err := NewKeychainSource(observe.ExecRunner{}, KeychainConfig{}); err == nil {
		t.Fatal("empty Keychain configuration accepted")
	}

	oversized := bytes.Repeat([]byte("x"), maxSecretBytes+1)
	runner := &fakeRunner{outputs: map[string][]byte{
		fakeKey(securityCommand, keychainArgs(testConfig().TOTPService)...): oversized,
	}}
	source, _ := NewKeychainSource(runner, testConfig())
	if credentials, err := source.ReadPritunl(context.Background()); err == nil ||
		credentials != nil {
		t.Fatalf("oversized ReadPritunl() = %v, %v", credentials, err)
	}
	if !allZero(oversized) {
		t.Fatal("oversized Keychain output was not cleared")
	}
}

func allZero(value []byte) bool {
	for _, item := range value {
		if item != 0 {
			return false
		}
	}
	return true
}
