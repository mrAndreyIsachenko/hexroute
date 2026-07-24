package main

import (
	"os"

	"github.com/mrAndreyIsachenko/hexroute/internal/sentinel"
)

func main() {
	os.Exit(sentinel.Run(os.Args[1:], os.Stdout, os.Stderr))
}
