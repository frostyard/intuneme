package main

import (
	"os"

	"github.com/frostyard/clix"
	"github.com/frostyard/intuneme/cmd"
	"github.com/frostyard/intuneme/internal/nshelper"
	pkgversion "github.com/frostyard/intuneme/internal/version"
)

var version = "dev"
var commit = "none"
var date = "unknown"
var builtBy = "local"

func main() {
	// The intuneme binary is also installed as the privileged helper. Dispatch
	// on argv[0] before Cobra sees the helper's arguments.
	if nshelper.IsMachineHelperName(os.Args[0]) {
		os.Exit(nshelper.MachineMain(os.Args[1:]))
	}
	if nshelper.IsHelperName(os.Args[0]) {
		os.Exit(nshelper.Main(os.Args[1:]))
	}

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
