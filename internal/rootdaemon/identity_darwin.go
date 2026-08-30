//go:build darwin

package rootdaemon

import (
	"strings"

	"golang.org/x/sys/unix"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

// bootIdentity returns the identity of the current boot session.
//
// The kernel mints it once per boot, which is exactly the property the read
// model needs: a monotonic freshness deadline is only comparable inside the
// boot that issued it, and a boot that changed must invalidate every deadline
// carried across it rather than have them silently look fresh.
func bootIdentity() string {
	boot, err := unix.Sysctl("kern.bootsessionuuid")
	if err != nil {
		return ""
	}
	parsed, err := metadata.ParseUUID(strings.ToLower(strings.TrimSpace(boot)))
	if err != nil {
		return ""
	}
	return string(parsed)
}
