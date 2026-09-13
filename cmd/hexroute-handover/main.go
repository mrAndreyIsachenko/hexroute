// Command hexroute-handover moves the tunnel from one runtime to the other, in
// the foreground, held by the operator who ran it.
package main

import (
	"os"

	"github.com/mrAndreyIsachenko/hexroute/internal/handovercli"
)

func main() { os.Exit(handovercli.Run(os.Args[1:], os.Stdout, os.Stderr)) }
