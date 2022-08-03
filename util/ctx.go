package util

import (
	"context"
	"os"
	"os/signal"
)

func ContextWithSigterm(ctx context.Context) context.Context {
	ctx, cancel := context.WithCancel(ctx)
	c := make(chan os.Signal, 1)

	signal.Notify(c, os.Interrupt)

	go func() {
		select {
		case <-c:
			cancel()
		case <-ctx.Done():
		}
	}()

	return ctx
}
