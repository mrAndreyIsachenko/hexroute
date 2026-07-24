package userobserve

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

const testPritunlCLI = "/Applications/Pritunl.app/Contents/Resources/pritunl-client"

func TestProfileParsesAuthoritativeConnectedStateWithoutExposingIdentity(t *testing.T) {
	runner := &fakeRunner{outputs: map[string][]byte{
		fakeKey(testPritunlCLI, "list"): []byte(
			"| ID | Name | Status | Mode | Online For | Virtual IP | Client Address |\n" +
				"| profile-01 | Example | Active | wg | 00:42:00 | - | 10.42.0.7/24 |\n",
		),
	}}
	observer, err := NewPritunlObserver(runner, testPritunlCLI)
	if err != nil {
		t.Fatalf("NewPritunlObserver() error: %v", err)
	}

	observation, err := observer.Profile(context.Background(), "profile-01")
	if err != nil {
		t.Fatalf("Profile() error: %v", err)
	}
	if !observation.Found ||
		observation.State != ProfileActive ||
		!observation.HasClientAddress ||
		!observation.Connected() {
		t.Fatalf("Profile() = %+v", observation)
	}
	if !reflect.DeepEqual(runner.calls, []runnerCall{{
		name: testPritunlCLI,
		args: []string{"list"},
	}}) {
		t.Fatalf("calls = %#v", runner.calls)
	}
}

func TestProfileActiveConnectingWithoutAddressIsNotConnected(t *testing.T) {
	runner := &fakeRunner{outputs: map[string][]byte{
		fakeKey(testPritunlCLI, "list"): []byte(
			"| Example | profile-01 | vpn.example.invalid | Active | wg | Connecting | - | - |\n",
		),
	}}
	observer, _ := NewPritunlObserver(runner, testPritunlCLI)

	observation, err := observer.Profile(context.Background(), "profile-01")
	if err != nil {
		t.Fatalf("Profile() error: %v", err)
	}
	if !observation.Connecting || observation.HasClientAddress || observation.Connected() {
		t.Fatalf("Profile() = %+v", observation)
	}
}

func TestProfileAbsentAndUnknownStateRemainNonConnected(t *testing.T) {
	runner := &fakeRunner{outputs: map[string][]byte{
		fakeKey(testPritunlCLI, "list"): []byte(
			"| Example | another-profile | vpn.example.invalid | Active | wg | 00:42:00 | - | 10.42.0.7/24 |\n",
		),
	}}
	observer, _ := NewPritunlObserver(runner, testPritunlCLI)

	observation, err := observer.Profile(context.Background(), "profile-01")
	if err != nil {
		t.Fatalf("Profile() error: %v", err)
	}
	if observation.Found || observation.State != ProfileUnknown || observation.Connected() {
		t.Fatalf("Profile() = %+v", observation)
	}
}

func TestProfileRejectsMalformedAddressAndIdentifier(t *testing.T) {
	runner := &fakeRunner{outputs: map[string][]byte{
		fakeKey(testPritunlCLI, "list"): []byte(
			"| Example | profile-01 | vpn.example.invalid | Active | wg | 00:42:00 | - | not-an-address |\n",
		),
	}}
	observer, _ := NewPritunlObserver(runner, testPritunlCLI)

	if _, err := observer.Profile(context.Background(), "profile-01"); !errors.Is(
		err,
		ErrInvalidObservation,
	) {
		t.Fatalf("Profile() error = %v, want %v", err, ErrInvalidObservation)
	}
	runner.calls = nil
	if _, err := observer.Profile(context.Background(), "profile id; stop"); !errors.Is(
		err,
		ErrInvalidObservation,
	) {
		t.Fatalf("Profile() invalid ID error = %v, want %v", err, ErrInvalidObservation)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("invalid profile ID invoked commands: %#v", runner.calls)
	}
}

func TestServiceUsesFixedSystemLabel(t *testing.T) {
	runner := &fakeRunner{outputs: map[string][]byte{
		fakeKey(launchctlCommand, "print", pritunlServiceTarget): []byte(
			"system/com.pritunl.service = {\n" +
				"\tstate = running\n" +
				"\tpid = 912\n" +
				"\tresource coalition = {\n" +
				"\t\tstate = active\n" +
				"\t}\n" +
				"}\n",
		),
	}}
	observer, _ := NewPritunlObserver(runner, testPritunlCLI)

	observation, err := observer.Service(context.Background())
	if err != nil {
		t.Fatalf("Service() error: %v", err)
	}
	if !observation.Loaded || !observation.Running || observation.PID != 912 {
		t.Fatalf("Service() = %+v", observation)
	}
	want := []runnerCall{{
		name: launchctlCommand,
		args: []string{"print", pritunlServiceTarget},
	}}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, want)
	}
}

func TestClientAddressRequiresExactPritunlAddressOnTUN(t *testing.T) {
	runner := &fakeRunner{outputs: map[string][]byte{
		fakeKey(testPritunlCLI, "list"): []byte(
			"| Example | profile-01 | vpn.example.invalid | Active | wg | 00:42:00 | - | 10.42.0.7/24 |\n",
		),
		fakeKey(ifconfigCommand): []byte(
			"en0: flags=8863<UP>\n" +
				"\tinet 192.0.2.5 netmask 0xffffff00\n" +
				"utun8: flags=8051<UP,POINTOPOINT,RUNNING,MULTICAST>\n" +
				"\tinet 10.42.0.7 --> 10.42.0.7 netmask 0xffffffff\n",
		),
	}}
	observer, _ := NewPritunlObserver(runner, testPritunlCLI)
	profile, err := observer.Profile(context.Background(), "profile-01")
	if err != nil {
		t.Fatalf("Profile() error: %v", err)
	}

	observation, err := observer.ClientAddress(context.Background(), profile)
	if err != nil {
		t.Fatalf("ClientAddress() error: %v", err)
	}
	if !observation.Present || observation.Interface != "utun8" {
		t.Fatalf("ClientAddress() = %+v", observation)
	}
}

func TestClientAddressRejectsProfileWithoutAddressBeforeCommand(t *testing.T) {
	runner := &fakeRunner{}
	observer, _ := NewPritunlObserver(runner, testPritunlCLI)

	if _, err := observer.ClientAddress(
		context.Background(),
		ProfileObservation{Found: true, State: ProfileInactive},
	); !errors.Is(err, ErrInvalidObservation) {
		t.Fatalf("ClientAddress() error = %v, want %v", err, ErrInvalidObservation)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("invalid profile invoked commands: %#v", runner.calls)
	}
}
