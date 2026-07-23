package main

import (
	"os"

	"github.com/mrAndreyIsachenko/hexroute/internal/rootdaemon"
)

func main() {
	os.Exit(rootdaemon.Run(os.Args[1:], os.Stdout, os.Stderr))
}
