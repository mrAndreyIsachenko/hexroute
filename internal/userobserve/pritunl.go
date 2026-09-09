package userobserve

import (
	"context"
	"net/netip"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
)

const (
	launchctlCommand     = "/bin/launchctl"
	ifconfigCommand      = "/sbin/ifconfig"
	pritunlServiceTarget = "system/com.pritunl.service"
)

var (
	profileIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
)

type ProfileState string

const (
	ProfileUnknown      ProfileState = "unknown"
	ProfileActive       ProfileState = "active"
	ProfileInactive     ProfileState = "inactive"
	ProfileConnecting   ProfileState = "connecting"
	ProfileDisconnected ProfileState = "disconnected"
)

type ProfileObservation struct {
	Found            bool
	State            ProfileState
	Connecting       bool
	HasClientAddress bool
	clientAddress    netip.Addr
}

// NewProfileObservation states an observation of a session.
//
// The address stays unexported so that a caller cannot state one the observer
// did not see. This is how it is set: the parser below builds every real
// observation through here, and anything reproducing one uses the same door.
// Without it the evidence a session offers about its own address could not be
// exercised outside this package at all.
func NewProfileObservation(
	found bool,
	state ProfileState,
	connecting bool,
	address netip.Addr,
) ProfileObservation {
	return ProfileObservation{
		Found:            found,
		State:            state,
		Connecting:       connecting,
		HasClientAddress: address.Is4(),
		clientAddress:    address,
	}
}

// ClientAddress is the address the session says is its own.
//
// It is exposed because the root runtime is asked to confirm the same
// observation about it, and cannot look for an address it is not told.
func (observation ProfileObservation) ClientAddress() (netip.Addr, bool) {
	if !observation.HasClientAddress || !observation.clientAddress.Is4() {
		return netip.Addr{}, false
	}
	return observation.clientAddress, true
}

func (observation ProfileObservation) Connected() bool {
	return observation.Found &&
		observation.State == ProfileActive &&
		!observation.Connecting &&
		observation.HasClientAddress
}

type ServiceObservation struct {
	Loaded  bool
	Running bool
	PID     int
}

type ClientAddressObservation struct {
	Present   bool
	Interface string
}

type PritunlObserver struct {
	runner observe.Runner
	cli    string
}

func NewPritunlObserver(runner observe.Runner, cli string) (*PritunlObserver, error) {
	if runner == nil ||
		!filepath.IsAbs(cli) ||
		filepath.Base(cli) != "pritunl-client" ||
		filepath.Clean(cli) != cli {
		return nil, ErrInvalidObservation
	}
	return &PritunlObserver{
		runner: runner,
		cli:    cli,
	}, nil
}

func (observer *PritunlObserver) Profile(
	ctx context.Context,
	profileID string,
) (ProfileObservation, error) {
	if !profileIDPattern.MatchString(profileID) {
		return ProfileObservation{}, ErrInvalidObservation
	}
	output, err := observer.runner.Output(ctx, observer.cli, "list")
	if err != nil {
		return ProfileObservation{}, err
	}
	return parseProfile(output, profileID)
}

func (observer *PritunlObserver) Service(ctx context.Context) (ServiceObservation, error) {
	output, err := observer.runner.Output(ctx, launchctlCommand, "print", pritunlServiceTarget)
	if err != nil {
		return ServiceObservation{}, err
	}
	return parseService(output)
}

func (observer *PritunlObserver) ClientAddress(
	ctx context.Context,
	profile ProfileObservation,
) (ClientAddressObservation, error) {
	if !profile.Found || !profile.HasClientAddress || !profile.clientAddress.Is4() {
		return ClientAddressObservation{}, ErrInvalidObservation
	}
	output, err := observer.runner.Output(ctx, ifconfigCommand)
	if err != nil {
		return ClientAddressObservation{}, err
	}
	return findClientAddress(output, profile.clientAddress)
}

func parseProfile(output []byte, profileID string) (ProfileObservation, error) {
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Split(line, "|")
		profileIndex := -1
		for index, field := range fields {
			if strings.TrimSpace(field) == profileID {
				profileIndex = index
				break
			}
		}
		if profileIndex < 0 {
			continue
		}
		if len(fields) <= profileIndex+6 {
			return ProfileObservation{}, ErrInvalidObservation
		}
		state := parseProfileState(fields[profileIndex+2])
		online := strings.TrimSpace(fields[profileIndex+4])
		clientAddress, hasAddress, err := parseClientAddress(fields[profileIndex+6])
		if err != nil {
			return ProfileObservation{}, err
		}
		if hasAddress != clientAddress.Is4() {
			// The two are the same statement, and a parse where they disagree
			// is one whose address is not what it says it has.
			return ProfileObservation{}, ErrInvalidObservation
		}
		return NewProfileObservation(
			true,
			state,
			state == ProfileConnecting || strings.EqualFold(online, "Connecting"),
			clientAddress,
		), nil
	}
	return ProfileObservation{
		Found: false,
		State: ProfileUnknown,
	}, nil
}

func parseProfileState(value string) ProfileState {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "active":
		return ProfileActive
	case "inactive":
		return ProfileInactive
	case "connecting":
		return ProfileConnecting
	case "disconnected":
		return ProfileDisconnected
	default:
		return ProfileUnknown
	}
}

func parseClientAddress(value string) (netip.Addr, bool, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "-" {
		return netip.Addr{}, false, nil
	}
	if prefix, err := netip.ParsePrefix(value); err == nil && prefix.Addr().Is4() {
		return prefix.Addr(), true, nil
	}
	if address, err := netip.ParseAddr(value); err == nil && address.Is4() {
		return address, true, nil
	}
	return netip.Addr{}, false, ErrInvalidObservation
}

func parseService(output []byte) (ServiceObservation, error) {
	fields := parseLaunchdFields(output)
	state, exists := fields["state"]
	if !exists {
		return ServiceObservation{}, ErrInvalidObservation
	}
	observation := ServiceObservation{
		Loaded:  true,
		Running: state == "running",
	}
	if value := fields["pid"]; value != "" {
		pid, err := strconv.Atoi(value)
		if err != nil || pid <= 0 {
			return ServiceObservation{}, ErrInvalidObservation
		}
		observation.PID = pid
	}
	if observation.Running && observation.PID == 0 {
		return ServiceObservation{}, ErrInvalidObservation
	}
	return observation, nil
}

func parseLaunchdFields(output []byte) map[string]string {
	fields := make(map[string]string)
	depth := 0
	for _, line := range strings.Split(string(output), "\n") {
		if depth == 1 {
			key, value, found := strings.Cut(line, "=")
			if found {
				key = strings.ToLower(strings.TrimSpace(key))
				value = strings.ToLower(strings.TrimSpace(value))
				if key != "" && value != "" {
					fields[key] = value
				}
			}
		}
		depth += strings.Count(line, "{")
		depth -= strings.Count(line, "}")
		if depth < 0 {
			return map[string]string{}
		}
	}
	return fields
}

func findClientAddress(output []byte, address netip.Addr) (ClientAddressObservation, error) {
	if !address.Is4() {
		return ClientAddressObservation{}, ErrInvalidObservation
	}
	// The same parse the root runtime uses to confirm this observation. One
	// question, one answer, or the second opinion refuses what the first asked.
	if name, present := observe.TunnelAddress(output, address); present {
		return ClientAddressObservation{Present: true, Interface: name}, nil
	}
	return ClientAddressObservation{}, nil
}
