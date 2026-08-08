package qualificationagent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/ipc"
	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

type LocalStatusReader struct {
	RootSocket string
	UserSocket string
	Timeout    time.Duration
}

func (reader LocalStatusReader) ReadPolicySnapshot(ctx context.Context) (PolicySnapshot, error) {
	root, err := reader.read(ctx, reader.RootSocket, policy.DomainRoot)
	if err != nil {
		return PolicySnapshot{}, ErrStatusUnavailable
	}
	user, err := reader.read(ctx, reader.UserSocket, policy.DomainUser)
	if err != nil {
		return PolicySnapshot{}, ErrStatusUnavailable
	}
	snapshot := PolicySnapshot{Root: root, User: user}
	if snapshot.Validate() != nil {
		return PolicySnapshot{}, ErrStatusUnavailable
	}
	return snapshot, nil
}

func (reader LocalStatusReader) read(
	ctx context.Context,
	path string,
	domain policy.Domain,
) (ipc.PolicyStatusResult, error) {
	requestID, err := randomRequestID()
	if err != nil {
		return ipc.PolicyStatusResult{}, ErrStatusUnavailable
	}
	request := ipc.Request{
		Version: ipc.ProtocolVersion, RequestID: requestID,
		Action: ipc.ActionPolicyStatus, PolicyStatus: &ipc.PolicyStatusRequest{},
	}
	timeout := reader.Timeout
	if timeout <= 0 || timeout > 10*time.Second {
		timeout = 5 * time.Second
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	response, err := (ipc.Client{Path: path, Timeout: timeout}).Do(requestCtx, request)
	if err != nil || !response.OK || response.PolicyStatus == nil ||
		response.PolicyStatus.Status.Domain != domain || response.PolicyStatus.Validate() != nil {
		return ipc.PolicyStatusResult{}, ErrStatusUnavailable
	}
	return *response.PolicyStatus, nil
}

func randomRequestID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}
