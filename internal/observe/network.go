package observe

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
)

const (
	routeCommand    = "/sbin/route"
	ifconfigCommand = "/sbin/ifconfig"
	pmsetCommand    = "/usr/bin/pmset"
	ioregCommand    = "/usr/sbin/ioreg"
)

var (
	ErrInvalidObservation = errors.New("invalid network observation")
	interfacePattern      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,31}$`)
)

type LinkState string

const (
	LinkStateUnknown LinkState = "unknown"
	LinkStateUp      LinkState = "up"
	LinkStateDown    LinkState = "down"
)

type PhysicalNetwork struct {
	Interface string
	Gateway   netip.Addr
	Link      LinkState
}

func (network PhysicalNetwork) Ready() bool {
	return network.Interface != "" && network.Gateway.IsValid() && network.Link == LinkStateUp
}

type TUNInterface struct {
	Name      string
	Addresses []netip.Addr
}

type RouteObservation struct {
	Requested   netip.Addr
	Destination netip.Addr
	Interface   string
	Gateway     netip.Addr
}

type PowerSource string

const (
	PowerSourceUnknown PowerSource = "unknown"
	PowerSourceAC      PowerSource = "ac"
	PowerSourceBattery PowerSource = "battery"
)

type LidState string

const (
	LidStateUnknown LidState = "unknown"
	LidStateOpen    LidState = "open"
	LidStateClosed  LidState = "closed"
)

type WakeKind string

const (
	WakeKindUnknown WakeKind = "unknown"
	WakeKindFull    WakeKind = "full"
	WakeKindDark    WakeKind = "dark"
)

type PowerObservation struct {
	Source   PowerSource
	Lid      LidState
	WakeKind WakeKind
}

func (power PowerObservation) MayPreventIdleSleep() bool {
	return power.Source == PowerSourceAC && power.Lid == LidStateOpen && power.WakeKind != WakeKindDark
}

type MacOSObserver struct {
	runner Runner
}

func NewMacOSObserver(runner Runner) (*MacOSObserver, error) {
	if runner == nil {
		return nil, errors.New("runner is required")
	}
	return &MacOSObserver{runner: runner}, nil
}

func (observer *MacOSObserver) PhysicalNetwork(
	ctx context.Context,
	expectedInterface string,
) (PhysicalNetwork, error) {
	routeArgs := []string{"-n", "get", "default"}
	if expectedInterface != "" {
		if !validInterface(expectedInterface) || strings.HasPrefix(expectedInterface, "utun") {
			return PhysicalNetwork{}, ErrInvalidObservation
		}
		routeArgs = []string{"-n", "get", "-ifscope", expectedInterface, "default"}
	}
	routeOutput, err := observer.runner.Output(ctx, routeCommand, routeArgs...)
	if err != nil {
		return PhysicalNetwork{}, err
	}
	route, err := parseRoute(routeOutput, netip.Addr{})
	if err != nil {
		return PhysicalNetwork{}, err
	}
	if !route.Gateway.IsValid() ||
		!validInterface(route.Interface) ||
		strings.HasPrefix(route.Interface, "utun") ||
		(expectedInterface != "" && route.Interface != expectedInterface) {
		return PhysicalNetwork{}, ErrInvalidObservation
	}

	linkOutput, err := observer.runner.Output(ctx, ifconfigCommand, route.Interface)
	if err != nil {
		return PhysicalNetwork{}, err
	}
	return PhysicalNetwork{
		Interface: route.Interface,
		Gateway:   route.Gateway,
		Link:      parseLinkState(linkOutput),
	}, nil
}

func (observer *MacOSObserver) TUNInterfaces(ctx context.Context) ([]TUNInterface, error) {
	output, err := observer.runner.Output(ctx, ifconfigCommand)
	if err != nil {
		return nil, err
	}
	return parseTUNInterfaces(output)
}

func (observer *MacOSObserver) Route(ctx context.Context, destination netip.Addr) (RouteObservation, error) {
	if !destination.IsValid() {
		return RouteObservation{}, ErrInvalidObservation
	}
	output, err := observer.runner.Output(ctx, routeCommand, "-n", "get", destination.String())
	if err != nil {
		return RouteObservation{}, err
	}
	return parseRoute(output, destination)
}

func (observer *MacOSObserver) Power(ctx context.Context) (PowerObservation, error) {
	batteryOutput, err := observer.runner.Output(ctx, pmsetCommand, "-g", "batt")
	if err != nil {
		return PowerObservation{}, err
	}
	lidOutput, err := observer.runner.Output(
		ctx,
		ioregCommand,
		"-r",
		"-k",
		"AppleClamshellState",
		"-d",
		"4",
	)
	if err != nil {
		return PowerObservation{}, err
	}
	powerOutput, err := observer.runner.Output(
		ctx,
		ioregCommand,
		"-r",
		"-n",
		"IOPMrootDomain",
		"-d",
		"1",
	)
	if err != nil {
		return PowerObservation{}, err
	}

	return PowerObservation{
		Source:   parsePowerSource(batteryOutput),
		Lid:      parseLidState(lidOutput),
		WakeKind: parseWakeKind(powerOutput),
	}, nil
}

func parseRoute(output []byte, requested netip.Addr) (RouteObservation, error) {
	fields := parseColonFields(output)
	var destination netip.Addr
	if destinationValue := fields["destination"]; destinationValue != "" && destinationValue != "default" {
		destination, _ = netip.ParseAddr(destinationValue)
	}

	var gateway netip.Addr
	if value := fields["gateway"]; value != "" {
		gateway, _ = netip.ParseAddr(value)
	}
	route := RouteObservation{
		Requested:   requested,
		Destination: destination,
		Interface:   fields["interface"],
		Gateway:     gateway,
	}
	if !validInterface(route.Interface) {
		return RouteObservation{}, ErrInvalidObservation
	}
	return route, nil
}

func parseColonFields(output []byte) map[string]string {
	fields := make(map[string]string)
	for _, line := range strings.Split(string(output), "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		key = strings.TrimSpace(strings.ToLower(key))
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			fields[key] = value
		}
	}
	return fields
}

func parseLinkState(output []byte) LinkState {
	normalized := strings.ToLower(string(output))
	switch {
	case strings.Contains(normalized, "status: active"):
		return LinkStateUp
	case strings.Contains(normalized, "status: inactive"):
		return LinkStateDown
	default:
		return LinkStateUnknown
	}
}

func parseTUNInterfaces(output []byte) ([]TUNInterface, error) {
	var interfaces []TUNInterface
	var current *TUNInterface

	for _, line := range strings.Split(string(output), "\n") {
		if line != "" && line[0] != '\t' && line[0] != ' ' {
			name, _, found := strings.Cut(line, ":")
			if !found || !strings.HasPrefix(name, "utun") {
				current = nil
				continue
			}
			if !validInterface(name) {
				return nil, ErrInvalidObservation
			}
			interfaces = append(interfaces, TUNInterface{Name: name})
			current = &interfaces[len(interfaces)-1]
			continue
		}
		if current == nil {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "inet ") && !strings.HasPrefix(trimmed, "inet6 ") {
			continue
		}
		parts := strings.Fields(trimmed)
		if len(parts) < 2 {
			continue
		}
		addressText := strings.Split(parts[1], "%")[0]
		address, err := netip.ParseAddr(addressText)
		if err == nil {
			current.Addresses = append(current.Addresses, address)
		}
	}
	return interfaces, nil
}

func parsePowerSource(output []byte) PowerSource {
	normalized := strings.ToLower(string(output))
	switch {
	case strings.Contains(normalized, "ac power"):
		return PowerSourceAC
	case strings.Contains(normalized, "battery power"):
		return PowerSourceBattery
	default:
		return PowerSourceUnknown
	}
}

func parseLidState(output []byte) LidState {
	normalized := strings.ToLower(string(output))
	switch {
	case strings.Contains(normalized, `"appleclamshellstate" = yes`):
		return LidStateClosed
	case strings.Contains(normalized, `"appleclamshellstate" = no`):
		return LidStateOpen
	default:
		return LidStateUnknown
	}
}

func parseWakeKind(output []byte) WakeKind {
	normalized := strings.ToLower(string(output))
	if strings.Contains(normalized, "darkwake") || strings.Contains(normalized, "dark wake") {
		return WakeKindDark
	}
	if strings.Contains(normalized, "wake type") || strings.Contains(normalized, "wake reason") {
		return WakeKindFull
	}
	return WakeKindUnknown
}

func validInterface(value string) bool {
	return interfacePattern.MatchString(value)
}

func FindTUNByAddress(interfaces []TUNInterface, address netip.Addr) (TUNInterface, error) {
	if !address.IsValid() {
		return TUNInterface{}, ErrInvalidObservation
	}
	for _, candidate := range interfaces {
		for _, candidateAddress := range candidate.Addresses {
			if candidateAddress == address {
				return candidate, nil
			}
		}
	}
	return TUNInterface{}, fmt.Errorf("%w: managed tun not found", ErrInvalidObservation)
}
