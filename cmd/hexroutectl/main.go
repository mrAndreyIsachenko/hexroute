package main

import (
	"os"

	"github.com/mrAndreyIsachenko/hexroute/internal/ctl"
)

func main() {
	config, err := ctl.DefaultConfig()
	if err != nil {
		os.Exit(1)
	}
	os.Exit(ctl.Run(os.Args[1:], os.Stdout, os.Stderr, config))
}
