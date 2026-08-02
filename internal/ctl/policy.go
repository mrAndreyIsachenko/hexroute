package ctl

import (
	"encoding/json"
	"flag"
	"io"

	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

func runPolicy(args []string, stdout, stderr io.Writer, config Config) int {
	if len(args) == 0 {
		writeGenericError(stderr)
		return 2
	}
	command := args[0]
	if command == "status" {
		if len(args) != 1 {
			writeGenericError(stderr)
			return 2
		}
		return runPolicyStatus(stdout, stderr, config)
	}
	identity, ok := parsePolicyIdentity(command, args[1:])
	if !ok {
		writeGenericError(stderr)
		return 2
	}
	switch command {
	case "prepare":
		return runPolicyPrepare(identity, stdout, stderr, config)
	case "commit", "rollback":
		return runPolicyCommit(command, identity, stdout, stderr, config)
	case "abort":
		return runPolicyAbort(identity, stdout, stderr, config)
	default:
		writeGenericError(stderr)
		return 2
	}
}

func runPolicyStatus(stdout, stderr io.Writer, config Config) int {
	results := make([]resultOutput, 0, 2)
	failed := false
	for _, role := range []ipc.DaemonRole{ipc.RoleRoot, ipc.RoleUser} {
		result, ok := roundTripRequest(role, ipc.Request{
			Action:       ipc.ActionPolicyStatus,
			PolicyStatus: &ipc.PolicyStatusRequest{},
		}, config)
		results = append(results, result)
		failed = failed || !ok
	}
	return writePolicyOutput("policy status", results, failed, stdout, stderr)
}

func runPolicyPrepare(
	identity ipc.PolicyTransactionIdentity,
	stdout, stderr io.Writer,
	config Config,
) int {
	results, ok := prepareBoth(identity, config)
	if !ok {
		abortBoth(identity, config)
	}
	return writePolicyOutput("policy prepare", results, !ok, stdout, stderr)
}

func runPolicyCommit(
	command string,
	identity ipc.PolicyTransactionIdentity,
	stdout, stderr io.Writer,
	config Config,
) int {
	prepared, ok := prepareBoth(identity, config)
	if !ok {
		abortBoth(identity, config)
		return writePolicyOutput("policy "+command, prepared, true, stdout, stderr)
	}
	results := make([]resultOutput, 0, 2)
	failed := false
	for _, role := range []ipc.DaemonRole{ipc.RoleRoot, ipc.RoleUser} {
		result, accepted := roundTripRequest(role, ipc.Request{
			Action:       ipc.ActionCommitPolicy,
			CommitPolicy: &ipc.CommitPolicyRequest{Transaction: identity},
		}, config)
		if accepted && (result.CommitPolicy == nil ||
			result.CommitPolicy.TransactionID != identity.TransactionID) {
			accepted = false
		}
		results = append(results, result)
		failed = failed || !accepted
	}
	return writePolicyOutput("policy "+command, results, failed, stdout, stderr)
}

func runPolicyAbort(
	identity ipc.PolicyTransactionIdentity,
	stdout, stderr io.Writer,
	config Config,
) int {
	results := make([]resultOutput, 0, 2)
	failed := false
	for _, role := range []ipc.DaemonRole{ipc.RoleRoot, ipc.RoleUser} {
		result, accepted := roundTripRequest(role, ipc.Request{
			Action:      ipc.ActionAbortPolicy,
			AbortPolicy: &ipc.AbortPolicyRequest{Transaction: identity},
		}, config)
		if accepted && (result.AbortPolicy == nil ||
			result.AbortPolicy.TransactionID != identity.TransactionID) {
			accepted = false
		}
		results = append(results, result)
		failed = failed || !accepted
	}
	return writePolicyOutput("policy abort", results, failed, stdout, stderr)
}

func prepareBoth(
	identity ipc.PolicyTransactionIdentity,
	config Config,
) ([]resultOutput, bool) {
	results := make([]resultOutput, 0, 2)
	ok := true
	for _, role := range []ipc.DaemonRole{ipc.RoleRoot, ipc.RoleUser} {
		result, accepted := roundTripRequest(role, ipc.Request{
			Action:        ipc.ActionPreparePolicy,
			PreparePolicy: &ipc.PreparePolicyRequest{Transaction: identity},
		}, config)
		if accepted && !receiptMatchesIdentity(result.PreparePolicy, identity, roleDomain(role)) {
			accepted = false
		}
		results = append(results, result)
		ok = ok && accepted
	}
	return results, ok
}

func abortBoth(identity ipc.PolicyTransactionIdentity, config Config) {
	for _, role := range []ipc.DaemonRole{ipc.RoleRoot, ipc.RoleUser} {
		_, _ = roundTripRequest(role, ipc.Request{
			Action:      ipc.ActionAbortPolicy,
			AbortPolicy: &ipc.AbortPolicyRequest{Transaction: identity},
		}, config)
	}
}

func receiptMatchesIdentity(
	receipt *ipc.PreparePolicyResult,
	identity ipc.PolicyTransactionIdentity,
	domain policy.Domain,
) bool {
	if receipt == nil {
		return false
	}
	policyGeneration := identity.RootPolicyGeneration
	payloadSHA256 := identity.RootPayloadSHA256
	if domain == policy.DomainUser {
		policyGeneration = identity.UserPolicyGeneration
		payloadSHA256 = identity.UserPayloadSHA256
	}
	return receipt.TransactionID == identity.TransactionID && receipt.Domain == domain &&
		receipt.BundleGeneration == identity.BundleGeneration &&
		receipt.PolicyGeneration == policyGeneration &&
		receipt.ManifestSHA256 == identity.ManifestSHA256 &&
		receipt.PayloadSHA256 == payloadSHA256 &&
		receipt.ApprovalSHA256 == identity.ApprovalSHA256
}

func writePolicyOutput(
	command string,
	results []resultOutput,
	failed bool,
	stdout, stderr io.Writer,
) int {
	if err := json.NewEncoder(stdout).Encode(commandOutput{
		Schema: outputSchema, Command: command, Results: results,
	}); err != nil {
		return 1
	}
	if failed {
		writeUnavailableError(stderr)
		return 1
	}
	return 0
}

func parsePolicyIdentity(
	name string,
	args []string,
) (ipc.PolicyTransactionIdentity, bool) {
	flags := flag.NewFlagSet("policy "+name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	transactionID := flags.String("transaction-id", "", "transaction UUID")
	bundleGeneration := &requiredUint64{}
	rootGeneration := &requiredUint64{}
	userGeneration := &requiredUint64{}
	flags.Var(bundleGeneration, "bundle-generation", "bundle generation")
	flags.Var(rootGeneration, "root-generation", "root policy generation")
	flags.Var(userGeneration, "user-generation", "user policy generation")
	manifestSHA256 := flags.String("manifest-sha256", "", "manifest digest")
	rootPayloadSHA256 := flags.String("root-payload-sha256", "", "root payload digest")
	userPayloadSHA256 := flags.String("user-payload-sha256", "", "user payload digest")
	approvalSHA256 := flags.String("approval-sha256", "", "approval digest")
	if flags.Parse(args) != nil || flags.NArg() != 0 ||
		!bundleGeneration.set || !rootGeneration.set || !userGeneration.set {
		return ipc.PolicyTransactionIdentity{}, false
	}
	identity := ipc.PolicyTransactionIdentity{
		TransactionID:        metadata.UUID(*transactionID),
		BundleGeneration:     bundleGeneration.value,
		RootPolicyGeneration: rootGeneration.value,
		UserPolicyGeneration: userGeneration.value,
		ManifestSHA256:       *manifestSHA256,
		RootPayloadSHA256:    *rootPayloadSHA256,
		UserPayloadSHA256:    *userPayloadSHA256,
		ApprovalSHA256:       *approvalSHA256,
	}
	return identity, identity.Validate() == nil
}

func roleDomain(role ipc.DaemonRole) policy.Domain {
	if role == ipc.RoleUser {
		return policy.DomainUser
	}
	return policy.DomainRoot
}
