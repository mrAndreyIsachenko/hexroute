// Command hexroute-soak-compare collects a soak of the tunnel decision rule and
// judges it against the runtime that owns the tunnel.
package main

import (
	"os"

	"github.com/mrAndreyIsachenko/hexroute/internal/soakcli"
)

func main() {
	os.Exit(soakcli.Run(os.Args[1:], os.Stdout, os.Stderr, nil))
}
