//go:build darwin

package userdaemon

import (
	"strings"

	"golang.org/x/sys/unix"

	"github.com/mrAndreyIsachenko/hexroute/internal/metadata"
)

// bootIdentity returns the identity of the current boot session. Facts carry
// it so the aggregate can refuse freshness deadlines from a boot that ended.
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
