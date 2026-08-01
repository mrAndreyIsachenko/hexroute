package main

import (
	"os"

	"github.com/mrAndreyIsachenko/hexroute/internal/ingressprobe"
)

func main() {
	os.Exit(ingressprobe.RunCLI(
		os.Args[1:],
		os.Stdin,
		os.Stdout,
		os.Stderr,
		ingressprobe.DefaultRunner(),
	))
}
