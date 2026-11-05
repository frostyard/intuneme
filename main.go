package main

import (
	"os"

	"github.com/frostyard/clix"
	"github.com/frostyard/intuneme/cmd"
	"github.com/frostyard/intuneme/internal/nshelper"
	"github.com/frostyard/intuneme/internal/problemrec"
	pkgversion "github.com/frostyard/intuneme/internal/version"
	"github.com/frostyard/std/problems"
)

var version = "dev"
var commit = "none"
var date = "unknown"
var builtBy = "local"

// problemOpts are the options of the problem recorder. Only tests set them
// (problems.WithSocket).
var problemOpts []problems.Option

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

	// Problem records go to the local journal only, and only from a release
	// build. They start after the helper dispatch above: the root helpers
	// never record. RecoverPanic must be deferred directly; it panics again,
	// so a panic still exits 2. A CrashOutput error changes nothing.
	rec := problemrec.Init(version, problemOpts...)
	defer rec.RecoverPanic()
	_ = rec.CrashOutput()

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
