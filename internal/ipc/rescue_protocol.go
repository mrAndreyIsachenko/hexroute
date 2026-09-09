package ipc

import (
	"errors"
	"net/netip"
)

// ErrInvalidRescueMessage is a rescue request whose evidence this layer cannot
// accept.
var ErrInvalidRescueMessage = errors.New("invalid IPC rescue message")

// RescuePritunlServiceRequest carries what the asking domain observed.
//
// It carries evidence, not a conclusion. The address is the one the session
// reports as its own and which the asking domain found on no interface; the
// answering domain looks for itself before acting on it. That is what keeps the
// second opinion a second opinion rather than a relay.
//
// It is optional. A request that names no address asks only about the service's
// own state, which is the older question and still a valid one.
type RescuePritunlServiceRequest struct {
	// UnreachableClientAddress is the session's own address, absent from every
	// tunnel interface as far as the asking domain could see.
	UnreachableClientAddress string `json:"unreachable_client_address,omitempty"`
}

// Validate enforces the bounds this layer owns.
func (request RescuePritunlServiceRequest) Validate() error {
	if request.UnreachableClientAddress == "" {
		return nil
	}
	address, err := netip.ParseAddr(request.UnreachableClientAddress)
	if err != nil || !address.Is4() || !address.IsValid() {
		return ErrInvalidRescueMessage
	}
	return nil
}
