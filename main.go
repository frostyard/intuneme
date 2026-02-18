package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/charmbracelet/fang"
	"github.com/frostyard/intuneme/cmd"
	pkgversion "github.com/frostyard/intuneme/internal/version"
)

var version = "dev"
var commit = "none"
var date = "unknown"
var builtBy = "local"

func makeVersionString() string {
	return fmt.Sprintf("%s (Commit: %s) (Date: %s) (Built by: %s)", version, commit, date, builtBy)
}

func main() {
	pkgversion.Version = version

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if err := fang.Execute(ctx, cmd.RootCmd(),
		fang.WithVersion(makeVersionString()),
		fang.WithNotifySignal(os.Interrupt),
	); err != nil {
		os.Exit(1)
	}
}
