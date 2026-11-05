// Package problemrec records the problems of the intuneme CLI in the local
// systemd journal, through github.com/frostyard/std/problems (schema
// frostyard.problem/1 of frostyard/core).
//
// It records 2 kinds of problem:
//
//   - a panic: main defers RecoverPanic on the process-wide recorder, and
//     CrashOutput turns the crash file of an earlier process that died of a
//     panic in another goroutine, or of a fatal error, into a record at the
//     next start;
//   - a coded internal error: Error, at the failure points named by the Code
//     constants. A code is recorded only for an internal failure after config
//     validation and sudo authentication passed: user setup and
//     authentication errors (not initialized, not running, failed sudo
//     authentication, a bad config, an untrusted helper source) are never
//     recorded.
//
// A record is 1 datagram to the local journal socket. The package sends
// nothing over a network, adds no flag, and writes nothing to stdout or
// stderr. A record that the journal cannot take is dropped. std caps the
// records of 1 process at 10 a minute.
//
// Only a release build records: ReleaseVersion turns the stamped version into
// the record version, and a development version makes the recorder inert.
//
// The recorder starts in main only after the dispatch of the privileged
// helpers (nshelper.Main, nshelper.MachineMain), so the root helpers never
// record.
package problemrec

import (
	"strings"
	"sync/atomic"

	"github.com/frostyard/std/problems"
)

// AppID is the frostyard app ID of intuneme in the problem record allowlist.
const AppID = "org.frostyard.intuneme"

// Component is the part of intuneme that records: the CLI.
const Component = problems.ComponentCLI

// Codes of the coded internal errors (FROSTYARD_CODE). A code is a stable
// contract: add a new code, never rename or reuse one. Record a code only
// where the failure cannot come from user config or sudo authentication.
const (
	// CodeBootFailed: systemd-nspawn did not start, or the machine was not
	// registered within 30 seconds.
	CodeBootFailed = "start.boot_failed"
	// CodeLingerFailed: start could not enable linger for the container
	// user (the already-running path or the cold-boot path).
	CodeLingerFailed = "start.linger_failed"
	// CodeProxyFailed: start could not start the broker, notify or
	// screencast proxy.
	CodeProxyFailed = "start.proxy_failed"
	// CodeSessionBusTimeout: open could not launch the app because the
	// container user session bus did not appear in time
	// (nspawn.ErrSessionBusTimeout).
	CodeSessionBusTimeout = "open.session_bus_timeout"
)

// Codes returns every code constant, for tests and docs.
func Codes() []string {
	return []string{CodeBootFailed, CodeLingerFailed, CodeProxyFailed, CodeSessionBusTimeout}
}

// ReleaseVersion returns the record version of the stamped build version v:
// v without 1 leading "v". GoReleaser stamps "0.21.0"; `make build` stamps
// `git describe` output such as "v0.21.0". A development version ("dev", a
// "-N-gHASH" or "-dirty" describe suffix, a snapshot version) is not a
// release semver after the trim, so the recorder made from it is inert and
// records nothing.
func ReleaseVersion(v string) string {
	return strings.TrimPrefix(v, "v")
}

// current is the process-wide recorder. Nil until Init: a nil recorder is
// inert.
var current atomic.Pointer[problems.Recorder]

// Init makes the process-wide recorder for the build version, stores it and
// returns it. Only tests give options (problems.WithSocket).
func Init(version string, opts ...problems.Option) *problems.Recorder {
	r := problems.New(AppID, ReleaseVersion(version), Component, opts...)
	current.Store(r)
	return r
}

// Set stores r as the process-wide recorder and returns the previous one.
// Tests use it to restore the old recorder.
func Set(r *problems.Recorder) *problems.Recorder {
	return current.Swap(r)
}

// Current returns the process-wide recorder, or nil before Init.
func Current() *problems.Recorder {
	return current.Load()
}

// Error records the coded internal error err under code. The error text goes
// only to the journal MESSAGE field. It never blocks, never fails, and does
// nothing when the recorder is nil or inert.
func Error(code string, err error) {
	current.Load().Error(code, err)
}
