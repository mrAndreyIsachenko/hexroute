package ingressobserver

import (
	"context"
	"io"
)

func Run(
	ctx context.Context,
	args []string,
	lookup LookupEnv,
	stderr io.Writer,
) int {
	if ctx == nil || len(args) != 0 || stderr == nil {
		return 64
	}
	config, err := LoadConfig(lookup)
	if err != nil {
		_, _ = io.WriteString(stderr, "observer configuration invalid\n")
		return 64
	}
	service, err := NewService(config)
	if err != nil {
		_, _ = io.WriteString(stderr, "observer configuration invalid\n")
		return 64
	}
	if err := service.Run(ctx); err != nil {
		_, _ = io.WriteString(stderr, "observer unavailable\n")
		return 1
	}
	return 0
}
