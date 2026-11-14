package main

import (
	"context"
	"fmt"
	"github.com/iakinsey/delver/application"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: delver CONFIG.json")
		os.Exit(2)
	}
	app, err := application.Load(os.Args[1])
	if err == nil {
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		err = application.Run(ctx, app)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
