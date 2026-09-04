package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/mhersson/contextmatrix-setup/internal/cli"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// run keeps os.Exit out of the frame holding the signal cleanup, so the
// deferred stop still runs on the error path.
func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	return cli.NewRootCmd().ExecuteContext(ctx)
}
