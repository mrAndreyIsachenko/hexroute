package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/mrAndreyIsachenko/hexroute/internal/ingressagent"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(ingressagent.Run(ctx, os.Args[1:], os.LookupEnv, os.Stdout, os.Stderr))
}
