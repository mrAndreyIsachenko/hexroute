package main

import (
	"os"

	"github.com/mrAndreyIsachenko/hexroute/internal/policycheck"
)

func main() {
	os.Exit(policycheck.Run(os.Args[1:], os.Stdout, os.Stderr))
}
