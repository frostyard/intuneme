package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/charmbracelet/fang"
	"github.com/frostyard/intuneme/cmd"
)

var version = "dev"
var commit = "none"
var date = "unknown"
var builtBy = "local"

func makeVersionString() string {
	return fmt.Sprintf("%s (Commit: %s) (Date: %s) (Built by: %s)", version, commit, date, builtBy)
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if err := fang.Execute(ctx, cmd.RootCmd(),
		fang.WithVersion(makeVersionString()),
		fang.WithNotifySignal(os.Interrupt),
	); err != nil {
		os.Exit(1)
	}
}
