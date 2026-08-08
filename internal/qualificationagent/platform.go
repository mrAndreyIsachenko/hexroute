package qualificationagent

import (
	"context"

	"github.com/mrAndreyIsachenko/hexroute/internal/userobserve"
)

type StatusReader interface {
	ReadPolicySnapshot(context.Context) (PolicySnapshot, error)
}

type Platform interface {
	Sample(context.Context) (PlatformSample, error)
	Wake(context.Context) (userobserve.WakeObservation, error)
}
