package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/iakinsey/delver/application"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	options := application.Options{}
	flag.StringVar(&options.Address, "listen", "", "Workspace listen address (default 127.0.0.1:8080)")
	flag.DurationVar(&options.StartupTimeout, "startup-timeout", 5*time.Minute, "Crawl runtime startup timeout")
	flag.Parse()
	if flag.NArg() > 1 {
		fmt.Fprintln(os.Stderr, "usage: delver [-listen ADDRESS] [CONFIG.json]")
		os.Exit(2)
	}
	if flag.NArg() == 1 {
		options.ImportPath = flag.Arg(0)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	err := application.ServeWorkspace(ctx, options)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
