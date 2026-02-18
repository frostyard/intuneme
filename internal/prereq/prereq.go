package prereq

import (
	"fmt"

	"github.com/frostyard/intuneme/internal/runner"
)

type requirement struct {
	binary  string
	pkgHint string
}

var requirements = []requirement{
	{"systemd-nspawn", "systemd-container"},
	{"machinectl", "systemd-container"},
}

// Check verifies all required binaries are available.
// Returns a list of errors for each missing binary.
func Check(r runner.Runner) []error {
	var errs []error
	for _, req := range requirements {
		if _, err := r.LookPath(req.binary); err != nil {
			errs = append(errs, fmt.Errorf("%s not found — install the %q package", req.binary, req.pkgHint))
		}
	}
	return errs
}
