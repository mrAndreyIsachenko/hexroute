//go:build darwin

package qualificationagent

import (
	"context"
	"strings"
	"time"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
	"github.com/mrAndreyIsachenko/hexroute/internal/observe"
	"github.com/mrAndreyIsachenko/hexroute/internal/policyclock"
	"github.com/mrAndreyIsachenko/hexroute/internal/userobserve"
	"golang.org/x/sys/unix"
)

type SystemPlatform struct {
	wake *userobserve.MacOSObserver
}

func NewSystemPlatform() (*SystemPlatform, error) {
	wake, err := userobserve.NewMacOSObserver(observe.ExecRunner{MaxOutput: 32 * 1024})
	if err != nil {
		return nil, ErrUnsupportedPlatform
	}
	return &SystemPlatform{wake: wake}, nil
}

func (platform *SystemPlatform) Sample(context.Context) (PlatformSample, error) {
	boot, err := unix.Sysctl("kern.bootsessionuuid")
	if err != nil {
		return PlatformSample{}, ErrUnsupportedPlatform
	}
	bootID, err := metadata.ParseUUID(strings.ToLower(strings.TrimSpace(boot)))
	if err != nil {
		return PlatformSample{}, ErrUnsupportedPlatform
	}
	monotonic, err := policyclock.ContinuousNow()
	if err != nil {
		return PlatformSample{}, ErrUnsupportedPlatform
	}
	return PlatformSample{
		BootID: bootID, ObservedAt: time.Now().UTC(), MonotonicNS: monotonic.Nanoseconds(),
	}, nil
}

func (platform *SystemPlatform) Wake(ctx context.Context) (userobserve.WakeObservation, error) {
	if platform == nil || platform.wake == nil {
		return userobserve.WakeObservation{}, ErrUnsupportedPlatform
	}
	return platform.wake.Clamshell(ctx)
}
