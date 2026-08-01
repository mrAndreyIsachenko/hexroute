package main

import (
	"fmt"
	"os"

	"github.com/mrAndreyIsachenko/hexroute/internal/releaseartifact"
)

func main() {
	if len(os.Args) != 3 {
		os.Exit(64)
	}
	if err := releaseartifact.BuildObserverArchive(os.Args[1], os.Args[2]); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "observer packaging failed: %v\n", err)
		os.Exit(1)
	}
}
