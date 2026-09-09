package observe

import (
	"net/netip"
	"regexp"
	"strings"
)

// TunnelInterfacePattern bounds what counts as an interface name, so a line
// that is not one cannot be read as the start of a block.
var TunnelInterfacePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,31}$`)

// TunnelAddress reports which tunnel interface carries an address, if any.
//
// Both domains ask this question and they have to answer it the same way. The
// user runtime asks it to decide whether a session that reports itself
// connected is carrying anything; the root runtime asks it to confirm that,
// before restarting a service on the strength of the answer. Two parses of one
// `ifconfig` would agree until one of them was edited, and the disagreement
// would look like the second opinion refusing what the first asked.
func TunnelAddress(output []byte, address netip.Addr) (string, bool) {
	if !address.Is4() {
		return "", false
	}
	currentInterface := ""
	for _, line := range strings.Split(string(output), "\n") {
		if line != "" && line[0] != ' ' && line[0] != '\t' {
			name, _, found := strings.Cut(line, ":")
			if !found ||
				!strings.HasPrefix(name, "utun") ||
				!TunnelInterfacePattern.MatchString(name) {
				currentInterface = ""
				continue
			}
			currentInterface = name
			continue
		}
		if currentInterface == "" {
			continue
		}
		parts := strings.Fields(strings.TrimSpace(line))
		if len(parts) >= 2 && parts[0] == "inet" && parts[1] == address.String() {
			return currentInterface, true
		}
	}
	return "", false
}
