package screencast

import (
	"context"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/frostyard/intuneme/internal/busproxy"
	"github.com/frostyard/intuneme/internal/busproxy/busproxytest"
	"github.com/godbus/dbus/v5"
)

// fakePortal stands in for the host xdg-desktop-portal. Like the real one, it
// builds request and session paths from the caller's unique name and the
// caller's tokens, and answers each request with a Response signal.
type fakePortal struct {
	conn *dbus.Conn

	mu         sync.Mutex
	selectOpts map[string]dbus.Variant
	startOpts  map[string]dbus.Variant
	parent     string
	closed     []dbus.ObjectPath
	sessions   []dbus.ObjectPath
}

func (f *fakePortal) requestPath(sender dbus.Sender, options map[string]dbus.Variant) dbus.ObjectPath {
	token, _ := options["handle_token"].Value().(string)
	return dbus.ObjectPath(requestPrefix + senderElement(string(sender)) + "/" + token)
}

// respond emits Response after the method has returned, as the real portal
// does once the user has answered.
func (f *fakePortal) respond(path dbus.ObjectPath, results map[string]dbus.Variant) {
	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = f.conn.Emit(path, RequestInterface+".Response", uint32(0), results)
	}()
}

func (f *fakePortal) CreateSession(sender dbus.Sender, options map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	token, _ := options["session_handle_token"].Value().(string)
	sessionPath := dbus.ObjectPath(sessionPrefix + senderElement(string(sender)) + "/" + token)
	if err := f.conn.Export(&fakeSession{f: f, path: sessionPath}, sessionPath, SessionInterface); err != nil {
		return "", dbus.MakeFailedError(err)
	}
	f.mu.Lock()
	f.sessions = append(f.sessions, sessionPath)
	f.mu.Unlock()
	path := f.requestPath(sender, options)
	f.respond(path, map[string]dbus.Variant{"session_handle": dbus.MakeVariant(string(sessionPath))})
	return path, nil
}

func (f *fakePortal) SelectSources(sender dbus.Sender, session dbus.ObjectPath, options map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	f.mu.Lock()
	f.selectOpts = options
	f.mu.Unlock()
	path := f.requestPath(sender, options)
	f.respond(path, map[string]dbus.Variant{})
	return path, nil
}

func (f *fakePortal) Start(sender dbus.Sender, session dbus.ObjectPath, parent string, options map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	f.mu.Lock()
	f.startOpts = options
	f.parent = parent
	f.mu.Unlock()
	path := f.requestPath(sender, options)
	f.respond(path, map[string]dbus.Variant{
		"streams": dbus.MakeVariant([]struct {
			ID    uint32
			Props map[string]dbus.Variant
		}{{ID: 42, Props: map[string]dbus.Variant{
			"source_type": dbus.MakeVariant(uint32(1)),
			"size":        dbus.MakeVariant(struct{ W, H int32 }{1920, 1080}),
			"x-extra":     dbus.MakeVariant("x"),
		}}}),
		"restore_token": dbus.MakeVariant("secret-token"),
	})
	return path, nil
}

func (f *fakePortal) OpenPipeWireRemote(sender dbus.Sender, session dbus.ObjectPath, options map[string]dbus.Variant) (dbus.UnixFD, *dbus.Error) {
	var fds [2]int
	if err := syscall.Pipe(fds[:]); err != nil {
		return -1, dbus.MakeFailedError(err)
	}
	_, _ = syscall.Write(fds[1], []byte("pipewire"))
	_ = syscall.Close(fds[1])
	// godbus does not close a sent fd. Close the read end after the reply.
	go func() {
		time.Sleep(time.Second)
		_ = syscall.Close(fds[0])
	}()
	return dbus.UnixFD(fds[0]), nil
}

type fakeSession struct {
	f    *fakePortal
	path dbus.ObjectPath
}

func (s *fakeSession) Close() *dbus.Error {
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	s.f.closed = append(s.f.closed, s.path)
	return nil
}

func (f *fakePortal) closedSessions() []dbus.ObjectPath {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]dbus.ObjectPath(nil), f.closed...)
}

type fakeProps struct{}

func (fakeProps) Get(iface, name string) (dbus.Variant, *dbus.Error) {
	all, _ := fakeProps{}.GetAll(iface)
	return all[name], nil
}

func (fakeProps) GetAll(iface string) (map[string]dbus.Variant, *dbus.Error) {
	return map[string]dbus.Variant{
		"AvailableSourceTypes": dbus.MakeVariant(uint32(3)),
		"AvailableCursorModes": dbus.MakeVariant(uint32(7)),
		"version":              dbus.MakeVariant(uint32(5)),
	}, nil
}

func connect(t *testing.T, sock string) *dbus.Conn {
	t.Helper()
	conn, _, err := busproxy.Dial(sock)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

type harness struct {
	portal        *fakePortal
	hostSock      string
	containerSock string
	cancel        context.CancelFunc
	done          chan error
}

// start runs serve between a fresh host bus with a fake portal and a fresh
// container bus. before runs on the container bus before the proxy starts.
func start(t *testing.T, before func(containerSock string)) *harness {
	t.Helper()
	hostSock, _ := busproxytest.StartBus(t)
	containerSock, _ := busproxytest.StartBus(t)

	portalConn := connect(t, hostSock)
	portal := &fakePortal{conn: portalConn}
	if err := portalConn.Export(portal, ObjectPath, ScreenCastInterface); err != nil {
		t.Fatal(err)
	}
	if err := portalConn.Export(fakeProps{}, ObjectPath, "org.freedesktop.DBus.Properties"); err != nil {
		t.Fatal(err)
	}
	if _, err := portalConn.RequestName(BusName, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatal(err)
	}
	if before != nil {
		before(containerSock)
	}

	proxyHost := connect(t, hostSock)
	proxyContainer := connect(t, containerSock)
	ctx, cancel := context.WithCancel(context.Background())
	h := &harness{portal: portal, hostSock: hostSock, containerSock: containerSock, cancel: cancel, done: make(chan error, 1)}
	ready := make(chan struct{})
	go func() {
		h.done <- serve(ctx, proxyHost, proxyContainer, func() error { close(ready); return nil })
	}()
	t.Cleanup(func() {
		cancel()
		if err := <-h.done; err != nil {
			t.Errorf("serve: %v", err)
		}
	})
	select {
	case <-ready:
	case err := <-h.done:
		t.Fatalf("serve: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("proxy did not become ready")
	}
	return h
}

// client is a container app such as Edge.
type client struct {
	t       *testing.T
	conn    *dbus.Conn
	signals chan *dbus.Signal
}

func newClient(t *testing.T, h *harness) *client {
	t.Helper()
	conn := connect(t, h.containerSock)
	// No match rule, like WebRTC, which subscribes to Response with
	// G_DBUS_SIGNAL_FLAGS_NO_MATCH_RULE. The proxy must address its signals
	// to the caller, or the bus does not deliver them.
	c := &client{t: t, conn: conn, signals: make(chan *dbus.Signal, 16)}
	conn.Signal(c.signals)
	return c
}

func (c *client) portal() dbus.BusObject { return c.conn.Object(BusName, ObjectPath) }

func (c *client) path(prefix, token string) dbus.ObjectPath {
	return dbus.ObjectPath(prefix + senderElement(c.conn.Names()[0]) + "/" + token)
}

// wait returns the next signal named name on path.
func (c *client) wait(path dbus.ObjectPath, name string) *dbus.Signal {
	c.t.Helper()
	timeout := time.After(3 * time.Second)
	for {
		select {
		case sig := <-c.signals:
			if sig.Path == path && sig.Name == name {
				return sig
			}
		case <-timeout:
			c.t.Fatalf("timed out waiting for %s on %s", name, path)
			return nil
		}
	}
}

// response calls method, checks that the returned request handle is the one
// the client computed, and returns the Response results.
func (c *client) response(token, method string, args ...any) map[string]dbus.Variant {
	c.t.Helper()
	want := c.path(requestPrefix, token)
	var handle dbus.ObjectPath
	if err := c.portal().Call(ScreenCastInterface+"."+method, 0, args...).Store(&handle); err != nil {
		c.t.Fatalf("%s: %v", method, err)
	}
	if handle != want {
		c.t.Fatalf("%s handle = %s, want %s", method, handle, want)
	}
	sig := c.wait(want, RequestInterface+".Response")
	if code := sig.Body[0].(uint32); code != 0 {
		c.t.Fatalf("%s response = %d", method, code)
	}
	return sig.Body[1].(map[string]dbus.Variant)
}

func (c *client) createSession(token string) dbus.ObjectPath {
	c.t.Helper()
	results := c.response("c"+token, "CreateSession", map[string]dbus.Variant{
		"handle_token":         dbus.MakeVariant("c" + token),
		"session_handle_token": dbus.MakeVariant(token),
	})
	got, _ := results["session_handle"].Value().(string)
	want := c.path(sessionPrefix, token)
	if dbus.ObjectPath(got) != want {
		c.t.Fatalf("session_handle = %q, want container path %s", got, want)
	}
	return want
}

func TestFullFlow(t *testing.T) {
	h := start(t, nil)
	c := newClient(t, h)

	var version uint32
	v, err := c.portal().GetProperty(ScreenCastInterface + ".version")
	if err != nil {
		t.Fatalf("version: %v", err)
	}
	if err := v.Store(&version); err != nil || version != 5 {
		t.Errorf("version = %v (%v), want the host's 5", v, err)
	}

	session := c.createSession("s1")

	c.response("sel", "SelectSources", session, map[string]dbus.Variant{
		"handle_token":  dbus.MakeVariant("sel"),
		"types":         dbus.MakeVariant(uint32(3)),
		"multiple":      dbus.MakeVariant(false),
		"cursor_mode":   dbus.MakeVariant(uint32(2)),
		"persist_mode":  dbus.MakeVariant(uint32(2)),
		"restore_token": dbus.MakeVariant("old"),
	})
	h.portal.mu.Lock()
	sel := h.portal.selectOpts
	h.portal.mu.Unlock()
	for _, key := range []string{"types", "multiple", "cursor_mode", "handle_token"} {
		if _, ok := sel[key]; !ok {
			t.Errorf("SelectSources option %s not passed to the host", key)
		}
	}
	for _, key := range []string{"persist_mode", "restore_token"} {
		if _, ok := sel[key]; ok {
			t.Errorf("SelectSources option %s passed to the host", key)
		}
	}
	if tok, _ := sel["handle_token"].Value().(string); tok == "sel" {
		t.Error("the container's handle_token reached the host, want the proxy's own")
	}

	results := c.response("st", "Start", session, "wayland:container-handle", map[string]dbus.Variant{
		"handle_token": dbus.MakeVariant("st"),
	})
	if _, ok := results["restore_token"]; ok {
		t.Error("restore_token reached the container")
	}
	streams, ok := results["streams"].Value().([][]any)
	if !ok || len(streams) != 1 || streams[0][0] != uint32(42) {
		t.Fatalf("streams = %#v, want node 42", results["streams"].Value())
	}
	props, _ := streams[0][1].(map[string]dbus.Variant)
	if props["source_type"].Value() != uint32(1) {
		t.Errorf("source_type = %v, want 1", props["source_type"].Value())
	}
	if size, _ := props["size"].Value().([]any); len(size) != 2 || size[0] != int32(1920) {
		t.Errorf("size = %#v, want (1920, 1080)", props["size"].Value())
	}
	if _, ok := props["x-extra"]; ok {
		t.Error("unknown stream property passed to the container")
	}
	h.portal.mu.Lock()
	parent := h.portal.parent
	h.portal.mu.Unlock()
	if parent != "" {
		t.Errorf("host got parent_window %q, want empty", parent)
	}

	var fd dbus.UnixFD
	if err := c.portal().Call(ScreenCastInterface+".OpenPipeWireRemote", 0, session, map[string]dbus.Variant{}).Store(&fd); err != nil {
		t.Fatalf("OpenPipeWireRemote: %v", err)
	}
	f := os.NewFile(uintptr(fd), "pipewire")
	defer func() { _ = f.Close() }()
	buf := make([]byte, 16)
	n, err := f.Read(buf)
	if err != nil || string(buf[:n]) != "pipewire" {
		t.Errorf("read from passed fd = %q, %v", buf[:n], err)
	}

	if err := c.conn.Object(BusName, session).Call(SessionInterface+".Close", 0).Err; err != nil {
		t.Fatalf("Session.Close: %v", err)
	}
	if closed := h.portal.closedSessions(); len(closed) != 1 {
		t.Errorf("host sessions closed = %v, want one", closed)
	}
}

func TestSessionBelongsToCaller(t *testing.T) {
	h := start(t, nil)
	owner := newClient(t, h)
	other := newClient(t, h)

	session := owner.createSession("s1")
	err := other.portal().Call(ScreenCastInterface+".SelectSources", 0, session, map[string]dbus.Variant{}).Err
	if err == nil {
		t.Error("another caller used the session")
	}
	err = other.portal().Call(ScreenCastInterface+".OpenPipeWireRemote", 0, session, map[string]dbus.Variant{}).Err
	if err == nil {
		t.Error("another caller opened the session's PipeWire remote")
	}
	if err := other.conn.Object(BusName, session).Call(SessionInterface+".Close", 0).Err; err == nil {
		t.Error("another caller closed the session")
	}
}

func TestSessionLimit(t *testing.T) {
	h := start(t, nil)
	c := newClient(t, h)

	for i := range maxSessions {
		c.createSession("s" + string(rune('a'+i)))
	}
	err := c.portal().Call(ScreenCastInterface+".CreateSession", 0, map[string]dbus.Variant{
		"handle_token": dbus.MakeVariant("over"),
	}).Err
	if err == nil {
		t.Errorf("session %d was created, want the limit of %d", maxSessions+1, maxSessions)
	}
}

func TestBadToken(t *testing.T) {
	h := start(t, nil)
	c := newClient(t, h)

	err := c.portal().Call(ScreenCastInterface+".CreateSession", 0, map[string]dbus.Variant{
		"handle_token": dbus.MakeVariant("../x"),
	}).Err
	if err == nil {
		t.Error("a token with path characters was accepted")
	}
}

func TestCallerGoneClosesHostSession(t *testing.T) {
	h := start(t, nil)
	c := newClient(t, h)
	c.createSession("s1")

	_ = c.conn.Close()
	deadline := time.Now().Add(3 * time.Second)
	for len(h.portal.closedSessions()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("host session not closed after the caller left")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestHostClosedReachesContainer(t *testing.T) {
	h := start(t, nil)
	c := newClient(t, h)
	session := c.createSession("s1")

	h.portal.mu.Lock()
	hostSession := h.portal.sessions[0]
	h.portal.mu.Unlock()
	if hostSession == session {
		t.Fatal("host and container session paths are equal, want them mapped")
	}
	if err := h.portal.conn.Emit(hostSession, SessionInterface+".Closed", map[string]dbus.Variant{}); err != nil {
		t.Fatal(err)
	}
	c.wait(session, SessionInterface+".Closed")
}

func TestOtherInterfacesMissing(t *testing.T) {
	h := start(t, nil)
	c := newClient(t, h)

	_, err := c.conn.Object(BusName, ObjectPath).GetProperty("org.freedesktop.portal.FileChooser.version")
	if err == nil {
		t.Error("FileChooser version read, want an error so Chromium uses its own dialog")
	}
	err = c.conn.Object(BusName, ObjectPath).Call("org.freedesktop.portal.OpenURI.OpenURI", 0,
		"", "https://example.com", map[string]dbus.Variant{}).Err
	if err == nil {
		t.Error("OpenURI call succeeded")
	}
}

func TestTakesOverContainerPortal(t *testing.T) {
	var old *dbus.Conn
	lost := make(chan *dbus.Signal, 4)
	h := start(t, func(containerSock string) {
		old = connect(t, containerSock)
		old.Signal(lost)
		reply, err := old.RequestName(BusName, dbus.NameFlagAllowReplacement)
		if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
			t.Fatalf("old portal RequestName = %v, %v", reply, err)
		}
	})

	timeout := time.After(3 * time.Second)
	for {
		select {
		case sig := <-lost:
			if sig.Name == "org.freedesktop.DBus.NameLost" && sig.Body[0] == BusName {
				c := newClient(t, h)
				var owner string
				if err := c.conn.BusObject().Call("org.freedesktop.DBus.GetNameOwner", 0, BusName).Store(&owner); err != nil {
					t.Fatal(err)
				}
				if owner == old.Names()[0] {
					t.Error("old portal still owns the name")
				}
				return
			}
		case <-timeout:
			t.Fatal("old portal did not lose the name")
		}
	}
}

func TestRefusesOwnerWithoutReplacement(t *testing.T) {
	hostSock, _ := busproxytest.StartBus(t)
	containerSock, _ := busproxytest.StartBus(t)
	holder := connect(t, containerSock)
	if _, err := holder.RequestName(BusName, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatal(err)
	}

	err := serve(context.Background(), connect(t, hostSock), connect(t, containerSock), func() error {
		t.Error("ready ran without owning the name")
		return nil
	})
	if err == nil {
		t.Fatal("serve succeeded while another process holds the name")
	}
}
