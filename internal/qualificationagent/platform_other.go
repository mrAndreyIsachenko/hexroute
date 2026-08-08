//go:build !darwin

package qualificationagent

import (
	"context"

	"github.com/mrAndreyIsachenko/hexroute/internal/userobserve"
)

type SystemPlatform struct{}

func NewSystemPlatform() (*SystemPlatform, error) { return nil, ErrUnsupportedPlatform }

func (*SystemPlatform) Sample(context.Context) (PlatformSample, error) {
	return PlatformSample{}, ErrUnsupportedPlatform
}

func (*SystemPlatform) Wake(context.Context) (userobserve.WakeObservation, error) {
	return userobserve.WakeObservation{}, ErrUnsupportedPlatform
}
