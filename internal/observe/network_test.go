package observe

import (
	"context"
	"errors"
	"net/netip"
	"reflect"
	"testing"
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

func (runner *fakeRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	key := name + "\x00" + joinArgs(args)
	runner.calls = append(runner.calls, runnerCall{name: name, args: append([]string(nil), args...)})
	if err := runner.errors[key]; err != nil {
		return nil, err
	}
	return runner.outputs[key], nil
}

func joinArgs(args []string) string {
	result := ""
	for index, arg := range args {
		if index != 0 {
			result += "\x00"
		}
		result += arg
	}
	return result
}

func fakeKey(name string, args ...string) string {
	return name + "\x00" + joinArgs(args)
}

func TestMacOSPhysicalNetworkUsesReadOnlyCommands(t *testing.T) {
	runner := &fakeRunner{outputs: map[string][]byte{
		fakeKey(routeCommand, "-n", "get", "-ifscope", "en7", "default"): []byte(
			"   route to: default\n" +
				"destination: default\n" +
				"    gateway: 192.0.2.1\n" +
				"  interface: en7\n",
		),
		fakeKey(ifconfigCommand, "en7"): []byte("en7: flags=8863<UP>\n\tstatus: active\n"),
	}}
	observer, err := NewMacOSObserver(runner)
	if err != nil {
		t.Fatalf("NewMacOSObserver() error: %v", err)
	}

	observation, err := observer.PhysicalNetwork(context.Background(), "en7")
	if err != nil {
		t.Fatalf("PhysicalNetwork() error: %v", err)
	}
	if !observation.Ready() || observation.Interface != "en7" ||
		observation.Gateway != netip.MustParseAddr("192.0.2.1") {
		t.Fatalf("PhysicalNetwork() = %+v", observation)
	}

	wantCalls := []runnerCall{
		{name: routeCommand, args: []string{"-n", "get", "-ifscope", "en7", "default"}},
		{name: ifconfigCommand, args: []string{"en7"}},
	}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, wantCalls)
	}
}

func TestPhysicalNetworkRejectsTUNAsPhysicalWithoutCommands(t *testing.T) {
	runner := &fakeRunner{}
	observer, _ := NewMacOSObserver(runner)

	if _, err := observer.PhysicalNetwork(context.Background(), "utun8"); !errors.Is(
		err,
		ErrInvalidObservation,
	) {
		t.Fatalf("PhysicalNetwork() error = %v, want %v", err, ErrInvalidObservation)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("invalid physical interface invoked commands: %#v", runner.calls)
	}
}

func TestParseTUNInterfacesAndFindManagedAddress(t *testing.T) {
	output := []byte(
		"en7: flags=8863<UP>\n" +
			"\tinet 192.0.2.5 netmask 0xffffff00\n" +
			"utun3: flags=8051<UP,POINTOPOINT,RUNNING,MULTICAST>\n" +
			"\tinet6 fe80::1%utun3 prefixlen 64 scopeid 0x18\n" +
			"utun8: flags=8051<UP,POINTOPOINT,RUNNING,MULTICAST>\n" +
			"\tinet 198.51.100.1 --> 198.51.100.1 netmask 0xffffffff\n",
	)
	interfaces, err := parseTUNInterfaces(output)
	if err != nil {
		t.Fatalf("parseTUNInterfaces() error: %v", err)
	}
	managed, err := FindTUNByAddress(interfaces, netip.MustParseAddr("198.51.100.1"))
	if err != nil {
		t.Fatalf("FindTUNByAddress() error: %v", err)
	}
	if managed.Name != "utun8" {
		t.Fatalf("managed TUN = %+v, want utun8", managed)
	}
}

func TestRouteRejectsInvalidDestinationBeforeCommand(t *testing.T) {
	runner := &fakeRunner{}
	observer, _ := NewMacOSObserver(runner)

	if _, err := observer.Route(context.Background(), netip.Addr{}); !errors.Is(err, ErrInvalidObservation) {
		t.Fatalf("Route() error = %v, want %v", err, ErrInvalidObservation)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("invalid route invoked commands: %#v", runner.calls)
	}
}

func TestParseRouteKeepsRequestedAndEffectiveDestinationSeparate(t *testing.T) {
	requested := netip.MustParseAddr("203.0.113.20")
	observation, err := parseRoute([]byte(
		"   route to: 203.0.113.20\n"+
			"destination: 203.0.113.0\n"+
			"    gateway: 192.0.2.1\n"+
			"  interface: en7\n",
	), requested)
	if err != nil {
		t.Fatalf("parseRoute() error: %v", err)
	}
	if observation.Requested != requested ||
		observation.Destination != netip.MustParseAddr("203.0.113.0") {
		t.Fatalf("parseRoute() = %+v", observation)
	}
}

func TestPowerObservationEnforcesKeepAwakePolicyInputs(t *testing.T) {
	runner := &fakeRunner{outputs: map[string][]byte{
		fakeKey(pmsetCommand, "-g", "batt"): []byte("Now drawing from 'AC Power'\n"),
		fakeKey(ioregCommand, "-r", "-k", "AppleClamshellState", "-d", "4"): []byte(
			`"AppleClamshellState" = No`,
		),
		fakeKey(ioregCommand, "-r", "-n", "IOPMrootDomain", "-d", "1"): []byte(
			`"Wake Type" = "UserActivity Assertion"`,
		),
	}}
	observer, _ := NewMacOSObserver(runner)

	observation, err := observer.Power(context.Background())
	if err != nil {
		t.Fatalf("Power() error: %v", err)
	}
	if observation.Source != PowerSourceAC ||
		observation.Lid != LidStateOpen ||
		observation.WakeKind != WakeKindFull ||
		!observation.MayPreventIdleSleep() {
		t.Fatalf("Power() = %+v", observation)
	}

	observation.WakeKind = WakeKindDark
	if observation.MayPreventIdleSleep() {
		t.Fatal("DarkWake unexpectedly permits keep-awake")
	}
}
