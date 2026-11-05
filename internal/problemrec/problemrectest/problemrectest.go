// Package problemrectest gives tests a local journal test socket for the
// problem records of intuneme. Only tests import it.
package problemrectest

import (
	"errors"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/frostyard/intuneme/internal/problemrec"
	"github.com/frostyard/std/problems"
)

// Socket is a unixgram test socket that stands in for the journal socket.
type Socket struct {
	// Path is the socket path for problems.WithSocket.
	Path string
	t    testing.TB
	conn *net.UnixConn
}

// NewSocket binds a unixgram socket in t.TempDir. It closes at the end of
// the test.
func NewSocket(t testing.TB) *Socket {
	t.Helper()
	path := filepath.Join(t.TempDir(), "j")
	c, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: path, Net: "unixgram"})
	if err != nil {
		t.Fatalf("listen on test socket: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return &Socket{Path: path, t: t, conn: c}
}

// Listen binds a test socket and returns its path and a function that reads
// the next record. next fails the test when no datagram comes within 5
// seconds, or when the datagram does not decode.
func Listen(t testing.TB) (path string, next func() map[string]string) {
	t.Helper()
	s := NewSocket(t)
	return s.Path, s.Next
}

// Next reads 1 datagram, with a deadline of 5 seconds, and decodes it with
// problems.DecodeEntry.
func (s *Socket) Next() map[string]string {
	s.t.Helper()
	if err := s.conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		s.t.Fatal(err)
	}
	buf := make([]byte, 1<<20)
	n, err := s.conn.Read(buf)
	if err != nil {
		s.t.Fatalf("read problem record: %v", err)
	}
	m, err := problems.DecodeEntry(buf[:n])
	if err != nil {
		s.t.Fatalf("decode problem record: %v", err)
	}
	return m
}

// ExpectNone fails the test when a datagram arrives within wait. A record is
// sent before the call that writes it returns, so a short wait is enough.
func (s *Socket) ExpectNone(wait time.Duration) {
	s.t.Helper()
	if err := s.conn.SetReadDeadline(time.Now().Add(wait)); err != nil {
		s.t.Fatal(err)
	}
	buf := make([]byte, 1<<20)
	n, err := s.conn.Read(buf)
	if err == nil {
		s.t.Fatalf("got an unexpected problem record: %q", buf[:n])
	}
	var ne net.Error
	if !errors.As(err, &ne) || !ne.Timeout() {
		s.t.Fatalf("read test socket: %v", err)
	}
}

// Use makes the process-wide recorder for version, writing to a new test
// socket, and restores the old recorder at the end of the test.
func Use(t testing.TB, version string) *Socket {
	t.Helper()
	s := NewSocket(t)
	old := problemrec.Current()
	problemrec.Init(version, problems.WithSocket(s.Path))
	t.Cleanup(func() { problemrec.Set(old) })
	return s
}

// Check fails the test when fields is not a valid intuneme record of class
// at version, or when its code differs from code (empty for a panic). It
// returns the validated detail.
func Check(t testing.TB, fields map[string]string, class, version, code string) problems.Detail {
	t.Helper()
	want := map[string]string{
		"MESSAGE_ID":            problems.MessageID,
		"FROSTYARD_SCHEMA":      problems.Schema,
		"FROSTYARD_APP_ID":      problemrec.AppID,
		"FROSTYARD_APP_VERSION": version,
		"FROSTYARD_RUNTIME":     "go",
		"FROSTYARD_COMPONENT":   string(problemrec.Component),
		"FROSTYARD_CLASS":       class,
	}
	if code != "" {
		want["FROSTYARD_CODE"] = code
	}
	for k, v := range want {
		if got := fields[k]; got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if code == "" {
		if _, ok := fields["FROSTYARD_CODE"]; ok {
			t.Errorf("FROSTYARD_CODE = %q, want none", fields["FROSTYARD_CODE"])
		}
	}
	d, err := problems.Validate(fields)
	if err != nil {
		t.Errorf("problems.Validate: %v", err)
	}
	return d
}
