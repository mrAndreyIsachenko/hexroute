package main

import (
	"os"

	"github.com/mrAndreyIsachenko/hexroute/internal/command"
)

func main() {
	os.Exit(command.Run("hexroutectl", os.Args[1:], os.Stdout, os.Stderr))
}
