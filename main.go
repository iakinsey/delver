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
	if len(os.Args) > 2 {
		fmt.Fprintln(os.Stderr, "usage: delver [CONFIG.json]")
		os.Exit(2)
	}
	options := application.Options{}
	if len(os.Args) == 2 {
		options.ImportPath = os.Args[1]
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	err := application.ServeWorkspace(ctx, options)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
