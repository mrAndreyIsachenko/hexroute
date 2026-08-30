package ctl

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/buildinfo"
	"github.com/mrAndreyIsachenko/hexroute/internal/control"
	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
)

const outputSchema = "hexroute.ctl.v1"

type RoundTripFunc func(context.Context, string, ipc.Request) (ipc.Response, error)
type RequestIDFunc func() (string, error)

type Config struct {
	RootSocket string
	UserSocket string
	RoundTrip  RoundTripFunc
	RequestID  RequestIDFunc
}

type commandOutput struct {
	Schema  string         `json:"schema"`
	Command string         `json:"command"`
	Results []resultOutput `json:"results"`
}

type resultOutput struct {
	Role          ipc.DaemonRole           `json:"role"`
	Available     bool                     `json:"available"`
	Error         ipc.ErrorCode            `json:"error,omitempty"`
	Status        *ipc.Status              `json:"status,omitempty"`
	Diagnostics   *ipc.Diagnostics         `json:"diagnostics,omitempty"`
	Resume        *ipc.ResumeResult        `json:"resume,omitempty"`
	PolicyStatus  *ipc.PolicyStatusResult  `json:"policy_status,omitempty"`
	PreparePolicy *ipc.PreparePolicyResult `json:"prepare_policy,omitempty"`
	CommitPolicy  *ipc.CommitPolicyResult  `json:"commit_policy,omitempty"`
	AbortPolicy   *ipc.AbortPolicyResult   `json:"abort_policy,omitempty"`
	// ReconcilerShadow reports what the reconciler's shadow store holds. It
	// is a description, not a handle: the store is synthetic-only and this
	// command cannot start, resume or cancel anything in it.
	ReconcilerShadow *ipc.ReconcilerShadowStatusResult `json:"reconciler_shadow,omitempty"`
}

type scope string

const (
	scopeRoot scope = "root"
	scopeUser scope = "user"
	scopeAll  scope = "all"
)

var ErrInvalidCommand = errors.New("invalid hexroutectl command")

func DefaultConfig() (Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, ErrInvalidCommand
	}
	userSocket, err := ipc.UserSocketPath(home)
	if err != nil {
		return Config{}, ErrInvalidCommand
	}
	return Config{
		RootSocket: ipc.RootSocketPath,
		UserSocket: userSocket,
		RoundTrip: func(ctx context.Context, path string, request ipc.Request) (ipc.Response, error) {
			return (ipc.Client{Path: path}).Do(ctx, request)
		},
		RequestID: randomRequestID,
	}, nil
}

func Run(args []string, stdout, stderr io.Writer, config Config) int {
	if stdout == nil || stderr == nil || !validConfig(config) {
		return 1
	}
	if len(args) == 1 && args[0] == "--version" {
		fmt.Fprintf(
			stdout,
			"hexroutectl version=%s commit=%s\n",
			buildinfo.Version,
			buildinfo.Commit,
		)
		return 0
	}
	if len(args) == 1 && args[0] == "--check" {
		return 0
	}
	if len(args) == 0 {
		writeGenericError(stderr)
		return 2
	}

	command := args[0]
	switch command {
	case "status", "diagnostics", "safe-mode":
		selected, ok := parseReadFlags(command, args[1:])
		if !ok {
			writeGenericError(stderr)
			return 2
		}
		action := ipc.ActionStatus
		if command == "diagnostics" {
			action = ipc.ActionExportDiagnostics
		}
		return runRead(command, selected, action, stdout, stderr, config)
	case "resume":
		selected, target, generation, ok := parseResumeFlags(args[1:])
		if !ok {
			writeGenericError(stderr)
			return 2
		}
		return runResume(selected, target, generation, stdout, stderr, config)
	case "reconciler-shadow":
		selected, ok := parseReadFlags(command, args[1:])
		if !ok {
			writeGenericError(stderr)
			return 2
		}
		return runShadow(selected, stdout, stderr, config)
	case "policy":
		return runPolicy(args[1:], stdout, stderr, config)
	default:
		writeGenericError(stderr)
		return 2
	}
}

func runRead(
	command string,
	selected scope,
	action ipc.Action,
	stdout io.Writer,
	stderr io.Writer,
	config Config,
) int {
	results := make([]resultOutput, 0, 2)
	failed := false
	for _, role := range roles(selected) {
		result, ok := roundTrip(role, action, "", 0, config)
		if command == "safe-mode" && result.Status != nil && !result.Status.SafeMode {
			result.Status = &ipc.Status{
				Role:       result.Status.Role,
				Mode:       result.Status.Mode,
				State:      result.Status.State,
				Generation: result.Status.Generation,
				SafeMode:   false,
			}
		}
		results = append(results, result)
		failed = failed || !ok
	}
	if err := json.NewEncoder(stdout).Encode(commandOutput{
		Schema:  outputSchema,
		Command: command,
		Results: results,
	}); err != nil {
		return 1
	}
	if failed {
		writeUnavailableError(stderr)
		return 1
	}
	return 0
}

func runResume(
	selected scope,
	target control.Component,
	generation uint64,
	stdout io.Writer,
	stderr io.Writer,
	config Config,
) int {
	role := ipc.RoleRoot
	if selected == scopeUser {
		role = ipc.RoleUser
	}
	result, ok := roundTrip(
		role,
		ipc.ActionResumeTarget,
		target,
		generation,
		config,
	)
	if err := json.NewEncoder(stdout).Encode(commandOutput{
		Schema:  outputSchema,
		Command: "resume",
		Results: []resultOutput{result},
	}); err != nil {
		return 1
	}
	if !ok {
		writeUnavailableError(stderr)
		return 1
	}
	return 0
}

func roundTrip(
	role ipc.DaemonRole,
	action ipc.Action,
	target control.Component,
	generation uint64,
	config Config,
) (resultOutput, bool) {
	request := ipc.Request{
		Action:             action,
		Target:             target,
		ExpectedGeneration: generation,
	}
	return roundTripRequest(role, request, config)
}

func roundTripRequest(
	role ipc.DaemonRole,
	request ipc.Request,
	config Config,
) (resultOutput, bool) {
	result := resultOutput{Role: role}
	requestID, err := config.RequestID()
	if err != nil {
		return result, false
	}
	request.Version = ipc.ProtocolVersion
	request.RequestID = requestID
	path := config.RootSocket
	if role == ipc.RoleUser {
		path = config.UserSocket
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	response, err := config.RoundTrip(ctx, path, request)
	if err != nil || response.RequestID != requestID {
		return result, false
	}
	result.Available = true
	result.Error = response.Error
	result.Status = response.Status
	result.Diagnostics = response.Diagnostics
	result.Resume = response.Resume
	result.PolicyStatus = response.PolicyStatus
	result.PreparePolicy = response.PreparePolicy
	result.CommitPolicy = response.CommitPolicy
	result.AbortPolicy = response.AbortPolicy
	result.ReconcilerShadow = response.ReconcilerShadowStatus
	if !response.OK {
		return result, false
	}
	if response.Status != nil && response.Status.Role != role {
		return resultOutput{Role: role}, false
	}
	if response.Diagnostics != nil && response.Diagnostics.Status.Role != role {
		return resultOutput{Role: role}, false
	}
	if response.Resume != nil && response.Resume.Role != role {
		return resultOutput{Role: role}, false
	}
	// A shadow status answering for the other daemon is refused rather than
	// shown: the two domains keep separate stores, and confusing them would
	// report one domain's evidence as the other's.
	if response.ReconcilerShadowStatus != nil &&
		response.ReconcilerShadowStatus.Role != role {
		return resultOutput{Role: role}, false
	}
	expectedDomain := roleDomain(role)
	if response.PolicyStatus != nil && response.PolicyStatus.Status.Domain != expectedDomain {
		return resultOutput{Role: role}, false
	}
	if response.PreparePolicy != nil && response.PreparePolicy.Domain != expectedDomain {
		return resultOutput{Role: role}, false
	}
	if response.CommitPolicy != nil && response.CommitPolicy.Status.Domain != expectedDomain {
		return resultOutput{Role: role}, false
	}
	if response.AbortPolicy != nil && response.AbortPolicy.Status.Domain != expectedDomain {
		return resultOutput{Role: role}, false
	}
	return result, true
}

func parseReadFlags(name string, args []string) (scope, bool) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	selected := flags.String("scope", string(scopeAll), "root, user, or all")
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		return "", false
	}
	value := scope(*selected)
	return value, value == scopeRoot || value == scopeUser || value == scopeAll
}

func parseResumeFlags(args []string) (scope, control.Component, uint64, bool) {
	flags := flag.NewFlagSet("resume", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	selected := flags.String("scope", "", "root or user")
	target := flags.String("target", "", "allowlisted component")
	generation := &requiredUint64{}
	flags.Var(generation, "generation", "expected state generation")
	if flags.Parse(args) != nil || flags.NArg() != 0 || !generation.set {
		return "", "", 0, false
	}
	selectedScope := scope(*selected)
	component := control.Component(*target)
	if selectedScope != scopeRoot && selectedScope != scopeUser {
		return "", "", 0, false
	}
	if !validResumeTarget(selectedScope, component) {
		return "", "", 0, false
	}
	return selectedScope, component, generation.value, true
}

type requiredUint64 struct {
	value uint64
	set   bool
}

func (value *requiredUint64) String() string {
	if value == nil {
		return ""
	}
	return strconv.FormatUint(value.value, 10)
}

func (value *requiredUint64) Set(raw string) error {
	parsed, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return ErrInvalidCommand
	}
	value.value = parsed
	value.set = true
	return nil
}

func roles(selected scope) []ipc.DaemonRole {
	switch selected {
	case scopeRoot:
		return []ipc.DaemonRole{ipc.RoleRoot}
	case scopeUser:
		return []ipc.DaemonRole{ipc.RoleUser}
	default:
		return []ipc.DaemonRole{ipc.RoleRoot, ipc.RoleUser}
	}
}

func validResumeTarget(selected scope, target control.Component) bool {
	if selected == scopeUser {
		return target == control.ComponentPritunl
	}
	switch target {
	case control.ComponentNetwork,
		control.ComponentTunnel,
		control.ComponentRoutes,
		control.ComponentCodex,
		control.ComponentTelegram:
		return true
	default:
		return false
	}
}

func validConfig(config Config) bool {
	return config.RootSocket != "" &&
		config.UserSocket != "" &&
		config.RoundTrip != nil &&
		config.RequestID != nil
}

func randomRequestID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func writeGenericError(writer io.Writer) {
	_, _ = io.WriteString(writer, "error: invalid hexroutectl command\n")
}

func writeUnavailableError(writer io.Writer) {
	_, _ = io.WriteString(writer, "error: one or more local control endpoints rejected the request\n")
}

// runShadow asks each selected daemon what its reconciler shadow store holds.
//
// The answer says the store is synthetic-only and exports no execution path.
// That is the point of asking: an operator can see that the reconciler is
// present and inert, rather than inferring it from the absence of evidence.
func runShadow(
	selected scope,
	stdout io.Writer,
	stderr io.Writer,
	config Config,
) int {
	results := make([]resultOutput, 0, 2)
	failed := false
	for _, role := range roles(selected) {
		result, ok := roundTripRequest(role, ipc.Request{
			Action:                 ipc.ActionReconcilerShadowStatus,
			ReconcilerShadowStatus: &ipc.ReconcilerShadowStatusRequest{},
		}, config)
		results = append(results, result)
		failed = failed || !ok
	}
	if err := json.NewEncoder(stdout).Encode(commandOutput{
		Schema:  outputSchema,
		Command: "reconciler-shadow",
		Results: results,
	}); err != nil {
		return 1
	}
	if failed {
		writeUnavailableError(stderr)
		return 1
	}
	return 0
}
