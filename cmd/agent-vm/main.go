package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"agent-vm/internal/cli"
	"agent-vm/internal/host"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := cli.New().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "agent-vm:", err)
		code := host.ExitCode(err)
		if errors.Is(err, context.Canceled) {
			code = 130
		}
		os.Exit(code)
	}
}
