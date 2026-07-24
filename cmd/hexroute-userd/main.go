package main

import (
	"os"

	"github.com/mrAndreyIsachenko/hexroute/internal/userdaemon"
)

func main() {
	os.Exit(userdaemon.Run(os.Args[1:], os.Stdout, os.Stderr))
}
