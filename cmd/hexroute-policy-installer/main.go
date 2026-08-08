package main

import (
	"os"

	"github.com/mrAndreyIsachenko/hexroute/internal/policyinstaller"
)

func main() {
	os.Exit(policyinstaller.Run(os.Args[1:], os.Stdout, os.Stderr))
}
