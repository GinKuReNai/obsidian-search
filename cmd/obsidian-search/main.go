package main

import (
	"context"
	"github.com/GinKuReNai/obsidian-search/internal/searchcli"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(searchcli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}
