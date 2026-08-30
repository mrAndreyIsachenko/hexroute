package ipc

import (
	"encoding/json"
	"errors"

	"github.com/mrAndreyIsachenko/hexroute/internal/policy"
)

// The user domain publishes what it observed; it never asks root to do
// anything. That is why this is a publication and not an action: there is no
// target, no generation to act on and no result that authorizes anything.

const (
	// MaxPublishedFacts bounds one publication.
	MaxPublishedFacts = 8
	// MaxPublishedFactBytes bounds one encoded fact inside a publication.
	MaxPublishedFactBytes = 4 * 1024
	// MaxPublishedTotalBytes bounds the whole set, so a caller cannot reach
	// the frame limit by sending the maximum count at the maximum size.
	MaxPublishedTotalBytes = 8 * 1024
)

var (
	ErrInvalidConnectivityMessage = errors.New("invalid IPC connectivity message")
	ErrConnectivityDomain         = errors.New("IPC connectivity domain is not permitted")
)

// PublishConnectivityFactsRequest carries canonically encoded facts from the
// user domain to the root aggregate.
//
// The facts travel as opaque bytes. This layer checks who is speaking, for
// which domain, and how much they sent; what the facts mean is the acceptor's
// question, and answering it here would put the model inside the transport.
type PublishConnectivityFactsRequest struct {
	Domain policy.Domain     `json:"domain"`
	BootID string            `json:"boot_id"`
	Facts  []json.RawMessage `json:"facts"`
}

// PublishConnectivityFactsResult reports what the aggregate did with them.
//
// It carries counts and a watermark. It does not return the accepted facts,
// a snapshot, a proposal or anything the caller could act on.
type PublishConnectivityFactsResult struct {
	Accepted      uint16 `json:"accepted"`
	Duplicates    uint16 `json:"duplicates"`
	Conflicts     uint16 `json:"conflicts"`
	Rejected      uint16 `json:"rejected"`
	HighWatermark uint64 `json:"high_watermark"`
}

// Validate enforces the bounds this layer owns.
func (request PublishConnectivityFactsRequest) Validate() error {
	// Only the user domain publishes over IPC. Root observes its own
	// components directly and has no reason to send them to itself.
	if request.Domain != policy.DomainUser {
		return ErrConnectivityDomain
	}
	if !validBootID(request.BootID) {
		return ErrInvalidConnectivityMessage
	}
	if len(request.Facts) == 0 || len(request.Facts) > MaxPublishedFacts {
		return ErrInvalidConnectivityMessage
	}
	total := 0
	for _, fact := range request.Facts {
		if len(fact) == 0 || len(fact) > MaxPublishedFactBytes {
			return ErrInvalidConnectivityMessage
		}
		total += len(fact)
	}
	if total > MaxPublishedTotalBytes {
		return ErrInvalidConnectivityMessage
	}
	return nil
}

// validBootID accepts the same bounded alphabet a fact's boot identity uses,
// without importing the model to ask.
func validBootID(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9', r == '-', r == '_', r == '.':
		default:
			return false
		}
	}
	return true
}

// valid reports whether a publication result could have come from an
// aggregate that folded a publication.
//
// A response is checked on its own, so this cannot compare the counts against
// what was sent. What it can refuse is a result that accounts for nothing, or
// for more facts than a publication may carry.
func (result PublishConnectivityFactsResult) valid() bool {
	total := int(result.Accepted) + int(result.Duplicates) +
		int(result.Conflicts) + int(result.Rejected)
	return total > 0 && total <= MaxPublishedFacts
}
