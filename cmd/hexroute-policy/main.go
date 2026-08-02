package main

import (
	"os"

	"github.com/mrAndreyIsachenko/hexroute/internal/policycli"
)

func main() {
	os.Exit(policycli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
