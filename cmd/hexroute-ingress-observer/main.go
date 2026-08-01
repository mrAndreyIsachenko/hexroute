package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/mrAndreyIsachenko/hexroute/internal/ingressobserver"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(ingressobserver.Run(ctx, os.Args[1:], os.LookupEnv, os.Stderr))
}
