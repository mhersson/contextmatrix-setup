package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"

	"github.com/mhersson/contextmatrix-setup/internal/cli"
	"github.com/mhersson/contextmatrix-setup/internal/engine"
)

func main() {
	if err := run(); err != nil {
		// The update's final line already reported its problems.
		var pe *engine.ProblemsError
		if !errors.As(err, &pe) {
			fmt.Fprintln(os.Stderr, "error:", err)
		}

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
