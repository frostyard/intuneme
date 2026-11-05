package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/frostyard/intuneme/cmd"
	"github.com/frostyard/intuneme/internal/problemrec/problemrectest"
	"github.com/frostyard/std/problems"
	"github.com/spf13/cobra"
)

// The problem record tests re-execute the test binary. The environment
// variable envChild makes TestMain run main() in a child mode instead of the
// tests.
const (
	envChild   = "INTUNEME_TEST_PROBLEM_CHILD"
	envSocket  = "INTUNEME_TEST_PROBLEM_SOCKET"
	envVersion = "INTUNEME_TEST_PROBLEM_VERSION"

	forcedPanicValue    = "intuneme test: forced panic"
	goroutinePanicValue = "intuneme test: goroutine panic"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv(envChild); mode != "" {
		runChild(mode)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// runChild runs main() as a release or dev build that writes to the test
// socket, with a hidden command for the mode.
func runChild(mode string) {
	version = os.Getenv(envVersion)
	problemOpts = []problems.Option{problems.WithSocket(os.Getenv(envSocket))}
	root := cmd.RootCmd()
	switch mode {
	case "panic":
		root.AddCommand(&cobra.Command{Use: "test-panic", Hidden: true, Run: func(*cobra.Command, []string) { forcedPanic() }})
		os.Args = []string{"intuneme", "test-panic"}
	case "goroutine":
		root.AddCommand(&cobra.Command{Use: "test-goroutine-panic", Hidden: true, Run: func(*cobra.Command, []string) { goroutinePanic() }})
		os.Args = []string{"intuneme", "test-goroutine-panic"}
	case "harmless":
		os.Args = []string{"intuneme", "--version"}
	default:
		os.Exit(99)
	}
	main()
}

func forcedPanic() {
	panic(forcedPanicValue)
}

// goroutinePanic panics in a new goroutine, where RecoverPanic of main cannot
// see it: only the crash output catches it.
func goroutinePanic() {
	go func() { panic(goroutinePanicValue) }()
	time.Sleep(time.Minute)
}

type childEnv struct {
	socket, version, state, home string
}

// runChildProcess runs the test binary in a child mode and returns its exit
// code and stderr.
func runChildProcess(t *testing.T, mode string, e childEnv) (int, string) {
	t.Helper()
	c := exec.Command(os.Args[0], "-test.run=^$")
	c.Env = append(os.Environ(),
		envChild+"="+mode,
		envSocket+"="+e.socket,
		envVersion+"="+e.version,
		"HOME="+e.home,
		"XDG_STATE_HOME="+e.state,
		"STATE_DIRECTORY=",
	)
	var stderr bytes.Buffer
	c.Stdout = &bytes.Buffer{}
	c.Stderr = &stderr
	err := c.Run()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("run child: %v", err)
		}
		code = ee.ExitCode()
	}
	return code, stderr.String()
}

func newChildEnv(t *testing.T, s *problemrectest.Socket, version string) childEnv {
	return childEnv{socket: s.Path, version: version, state: t.TempDir(), home: t.TempDir()}
}

// testedMain is the frame prefix of package main in a test binary: go test
// compiles the package under test with its import path, and only the
// generated test main is "main". A release binary has "main." frames only.
const testedMain = "github.com/frostyard/intuneme."

// checkFrames fails when frames is empty, has no frame of package main, or
// holds a file path, a line or an offset.
func checkFrames(t *testing.T, frames []string) {
	t.Helper()
	if len(frames) == 0 {
		t.Fatal("no frames")
	}
	hasMain := false
	for _, f := range frames {
		if strings.HasPrefix(f, "main.") || strings.HasPrefix(f, testedMain) {
			hasMain = true
		}
		if strings.Contains(f, ".go:") || strings.HasSuffix(f, ".go") || strings.Contains(f, "+0x") || strings.Contains(f, "(0x") {
			t.Errorf("frame %q holds a path, a line or an offset", f)
		}
	}
	if !hasMain {
		t.Errorf("no frame of package main: %q", frames)
	}
}

func TestPanicIsRecorded(t *testing.T) {
	s := problemrectest.NewSocket(t)
	code, stderr := runChildProcess(t, "panic", newChildEnv(t, s, "v0.21.0"))
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr:\n%s", code, stderr)
	}
	if !strings.Contains(stderr, forcedPanicValue) || !strings.Contains(stderr, "[recovered, repanicked]") {
		t.Fatalf("stderr lost the panic:\n%s", stderr)
	}
	f := s.Next()
	d := problemrectest.Check(t, f, "panic", "0.21.0", "")
	if f["FROSTYARD_FRAMES"] == "" {
		t.Fatal("FROSTYARD_FRAMES is empty")
	}
	checkFrames(t, strings.Split(f["FROSTYARD_FRAMES"], "\n"))
	checkFrames(t, d.Frames)
	if f["MESSAGE"] != forcedPanicValue {
		t.Errorf("MESSAGE = %q, want %q", f["MESSAGE"], forcedPanicValue)
	}
	s.ExpectNone(100 * time.Millisecond)
}

func TestCrashOutputIsRecordedAtNextStart(t *testing.T) {
	s := problemrectest.NewSocket(t)
	e := newChildEnv(t, s, "v0.21.0")
	code, stderr := runChildProcess(t, "goroutine", e)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr:\n%s", code, stderr)
	}
	if !strings.Contains(stderr, goroutinePanicValue) {
		t.Fatalf("stderr lost the panic:\n%s", stderr)
	}
	// RecoverPanic cannot see a panic of another goroutine.
	s.ExpectNone(100 * time.Millisecond)
	crashFiles, _ := filepath.Glob(filepath.Join(e.state, "frostyard-problems", "org.frostyard.intuneme", "*.crash"))
	if len(crashFiles) == 0 {
		t.Fatal("the crash left no crash file")
	}

	code, stderr = runChildProcess(t, "harmless", e)
	if code != 0 {
		t.Fatalf("harmless run exit code = %d, want 0; stderr:\n%s", code, stderr)
	}
	f := s.Next()
	d := problemrectest.Check(t, f, "panic", "0.21.0", "")
	checkFrames(t, d.Frames)
	if !strings.Contains(f["MESSAGE"], goroutinePanicValue) {
		t.Errorf("MESSAGE = %q, want %q", f["MESSAGE"], goroutinePanicValue)
	}
	s.ExpectNone(100 * time.Millisecond)
}

func TestDevBuildRecordsNothing(t *testing.T) {
	s := problemrectest.NewSocket(t)
	e := newChildEnv(t, s, "dev")
	code, stderr := runChildProcess(t, "panic", e)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2; stderr:\n%s", code, stderr)
	}
	if !strings.Contains(stderr, forcedPanicValue) {
		t.Fatalf("stderr lost the panic:\n%s", stderr)
	}
	s.ExpectNone(200 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(e.state, "frostyard-problems")); !os.IsNotExist(err) {
		t.Errorf("a dev build made the crash folder: %v", err)
	}
}
