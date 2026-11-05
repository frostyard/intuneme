package problemrec_test

import (
	"errors"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/frostyard/intuneme/internal/problemrec"
	"github.com/frostyard/intuneme/internal/problemrec/problemrectest"
	"github.com/frostyard/std/problems"
)

func TestReleaseVersion(t *testing.T) {
	tests := []struct {
		in, want string
		valid    bool
	}{
		{"v0.21.0", "0.21.0", true},
		{"0.21.0", "0.21.0", true},
		{"v1.2.3-rc.1", "1.2.3-rc.1", true},
		{"dev", "dev", false},
		{"", "", false},
		{"v0.21.0-2-gabc1234", "0.21.0-2-gabc1234", false},
		{"v0.21.0-dirty", "0.21.0-dirty", false},
		{"v0.21.0-2-gabc1234-dirty", "0.21.0-2-gabc1234-dirty", false},
		{"0.21.1-SNAPSHOT-abc1234", "0.21.1-SNAPSHOT-abc1234", false},
		{"0.21.0-next", "0.21.0-next", false},
		{"abc1234", "abc1234", false},
		{"vv0.21.0", "v0.21.0", false},
	}
	for _, tt := range tests {
		got := problemrec.ReleaseVersion(tt.in)
		if got != tt.want {
			t.Errorf("ReleaseVersion(%q) = %q, want %q", tt.in, got, tt.want)
		}
		// An inert recorder records nothing, so this needs no socket.
		r := problemrec.Init(tt.in)
		if (r.Err() == nil) != tt.valid {
			t.Errorf("Init(%q).Err() = %v, want valid=%v", tt.in, r.Err(), tt.valid)
		}
	}
	problemrec.Set(nil)
}

func TestInitDevIsInert(t *testing.T) {
	s := problemrectest.NewSocket(t)
	old := problemrec.Current()
	t.Cleanup(func() { problemrec.Set(old) })
	r := problemrec.Init("dev", problems.WithSocket(s.Path))
	if r.Err() == nil {
		t.Fatal("a dev build made an active recorder")
	}
	if problemrec.Current() != r {
		t.Fatal("Init did not store the recorder")
	}
	problemrec.Error(problemrec.CodeBootFailed, errors.New("boom"))
	s.ExpectNone(50 * time.Millisecond)
}

func TestNilRecorderIsInert(t *testing.T) {
	old := problemrec.Set(nil)
	t.Cleanup(func() { problemrec.Set(old) })
	// Must not panic.
	problemrec.Error(problemrec.CodeBootFailed, errors.New("boom"))
}

func TestErrorRecord(t *testing.T) {
	s := problemrectest.Use(t, "v0.21.0")
	if err := problemrec.Current().Err(); err != nil {
		t.Fatalf("release recorder is inert: %v", err)
	}
	problemrec.Error(problemrec.CodeLaunchFailed, errors.New("exec in container failed: exit status 1"))
	f := s.Next()
	d := problemrectest.Check(t, f, "error", "0.21.0", problemrec.CodeLaunchFailed)
	if d.Code != problemrec.CodeLaunchFailed {
		t.Errorf("detail code = %q", d.Code)
	}
	if got := f["MESSAGE"]; got != "exec in container failed: exit status 1" {
		t.Errorf("MESSAGE = %q", got)
	}
	if got := f["PRIORITY"]; got != "3" {
		t.Errorf("PRIORITY = %q, want 3", got)
	}
	s.ExpectNone(20 * time.Millisecond)
}

func TestSetRestores(t *testing.T) {
	old := problemrec.Set(nil)
	r := problemrec.Init("v0.21.0")
	if prev := problemrec.Set(old); prev != r {
		t.Fatal("Set did not return the previous recorder")
	}
	if problemrec.Current() != old {
		t.Fatal("Set did not restore the old recorder")
	}
}

func TestCodesMatchPattern(t *testing.T) {
	re := regexp.MustCompile(`^[a-z][a-z0-9_.]{0,63}$`)
	seen := map[string]bool{}
	for _, c := range problemrec.Codes() {
		if !re.MatchString(c) {
			t.Errorf("code %q does not match %s", c, re)
		}
		if seen[c] {
			t.Errorf("code %q is repeated", c)
		}
		seen[c] = true
	}
	if len(seen) != 5 {
		t.Errorf("got %d codes, want 5", len(seen))
	}
}

// The recorder writes only to the local journal socket: the package must not
// import a network package.
func TestNoNetworkImports(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		for _, imp := range f.Imports {
			p, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if p == "net" || strings.HasPrefix(p, "net/") || p == "crypto/tls" || p == "os/exec" ||
				strings.HasPrefix(p, "golang.org/x/net") || strings.Contains(p, "grpc") {
				t.Errorf("%s imports %s", path, p)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no non-test files")
	}
}
