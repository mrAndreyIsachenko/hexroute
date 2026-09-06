package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/mrAndreyIsachenko/hexroute/internal/configpublish"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(configpublish.Run(ctx, os.Args[1:], os.LookupEnv, os.Stdout, os.Stderr))
}
