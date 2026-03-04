package main

import (
	"os"

	"github.com/frostyard/clix"
	"github.com/frostyard/intuneme/cmd"
	pkgversion "github.com/frostyard/intuneme/internal/version"
)

var version = "dev"
var commit = "none"
var date = "unknown"
var builtBy = "local"

func main() {
	pkgversion.Version = version

	app := clix.App{
		Version: version,
		Commit:  commit,
		Date:    date,
		BuiltBy: builtBy,
	}

	if err := app.Run(cmd.RootCmd()); err != nil {
		os.Exit(1)
	}
}
