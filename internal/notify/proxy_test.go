package notify

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/frostyard/intuneme/internal/busproxy"
	"github.com/frostyard/intuneme/internal/busproxy/busproxytest"
	"github.com/godbus/dbus/v5"
)

// fakeServer stands in for the host notification server.
type fakeServer struct {
	mu         sync.Mutex
	icon       string
	hints      map[string]dbus.Variant
	replacesID uint32
	closed     []uint32
	// conn, when set, emits ActionInvoked for the new ID before Notify
	// returns, like a host server that is quick to report a click.
	conn *dbus.Conn
}

func (s *fakeServer) GetCapabilities() ([]string, *dbus.Error) {
	return []string{"actions", "body", "body-images", "persistence"}, nil
}

func (s *fakeServer) GetServerInformation() (string, string, string, string, *dbus.Error) {
	return "fake", "test", "1", "1.2", nil
}

func (s *fakeServer) Notify(appName string, replacesID uint32, appIcon, summary, body string,
	actions []string, hints map[string]dbus.Variant, expireTimeout int32) (uint32, *dbus.Error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.icon = appIcon
	s.hints = hints
	s.replacesID = replacesID
	if s.conn != nil {
		_ = s.conn.Emit(ObjectPath, InterfaceName+".ActionInvoked", uint32(7), "early")
		// Give the proxy time to receive the signal before the reply.
		time.Sleep(100 * time.Millisecond)
	}
	return 7, nil
}

func (s *fakeServer) CloseNotification(id uint32) *dbus.Error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = append(s.closed, id)
	return nil
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

// startProxy runs serve between a fresh host bus with a fake server and a
// fresh container bus. It returns the fake server, its connection, and a
// client connection on the container bus.
func startProxy(t *testing.T) (*fakeServer, *dbus.Conn, *dbus.Conn, *iconStore) {
	t.Helper()
	hostSock, _ := busproxytest.StartBus(t)
	containerSock, _ := busproxytest.StartBus(t)

	fake := &fakeServer{}
	fakeConn := connect(t, hostSock)
	if err := fakeConn.Export(fake, ObjectPath, InterfaceName); err != nil {
		t.Fatalf("export fake: %v", err)
	}
	if _, err := fakeConn.RequestName(BusName, dbus.NameFlagDoNotQueue); err != nil {
		t.Fatalf("fake RequestName: %v", err)
	}

	// Connect before registering the cancel cleanup. Cleanups run last in,
	// first out, so serve stops before its connections close.
	proxyHost := connect(t, hostSock)
	proxyContainer := connect(t, containerSock)
	icons, root := testStore(t)
	writeFile(t, filepath.Join(root, "tmp/d/logo.png"), "logo")
	writeFile(t, filepath.Join(root, "tmp/d/icon.png"), "icon")
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- serve(ctx, proxyHost, proxyContainer, icons) }()
	t.Cleanup(func() {
		cancel()
		if err := <-errCh; err != nil {
			t.Errorf("serve: %v", err)
		}
	})

	client := connect(t, containerSock)
	for range 50 {
		var owned bool
		_ = client.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, BusName).Store(&owned)
		if owned {
			return fake, fakeConn, client, icons
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("proxy did not claim the bus name on the container bus")
	return nil, nil, nil, nil
}

func TestGetCapabilitiesDropsBodyImages(t *testing.T) {
	_, _, client, _ := startProxy(t)

	var caps []string
	if err := client.Object(BusName, ObjectPath).Call(InterfaceName+".GetCapabilities", 0).Store(&caps); err != nil {
		t.Fatalf("GetCapabilities: %v", err)
	}
	want := []string{"actions", "body", "persistence"}
	if !slices.Equal(caps, want) {
		t.Errorf("capabilities = %v, want %v", caps, want)
	}
}

func TestGetServerInformation(t *testing.T) {
	_, _, client, _ := startProxy(t)

	var name, vendor, version, spec string
	err := client.Object(BusName, ObjectPath).Call(InterfaceName+".GetServerInformation", 0).
		Store(&name, &vendor, &version, &spec)
	if err != nil {
		t.Fatalf("GetServerInformation: %v", err)
	}
	if name != "fake" || spec != "1.2" {
		t.Errorf("server information = %q %q %q %q", name, vendor, version, spec)
	}
}

func TestNotifyCopiesIcons(t *testing.T) {
	fake, fakeConn, client, icons := startProxy(t)

	var id uint32
	err := client.Object(BusName, ObjectPath).Call(InterfaceName+".Notify", 0,
		"Microsoft Edge", uint32(0), "file:///tmp/d/logo.png", "New mail", "Hello",
		[]string{"default", "Activate"},
		map[string]dbus.Variant{"image-path": dbus.MakeVariant("/tmp/d/icon.png")},
		int32(-1),
	).Store(&id)
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if id != 7 {
		t.Errorf("id = %d, want the host server's 7", id)
	}

	fake.mu.Lock()
	logo, ok := strings.CutPrefix(fake.icon, "file://")
	image, _ := fake.hints["image-path"].Value().(string)
	fake.mu.Unlock()
	if !ok {
		t.Fatalf("host got app_icon %q, want a file:// URL", fake.icon)
	}
	assertCopy(t, icons, logo, "logo")
	assertCopy(t, icons, image, "icon")

	// The close signal removes the copies. Wait for the client to see the
	// relayed signal, so the proxy has handled it.
	if err := client.AddMatchSignal(dbus.WithMatchInterface(InterfaceName)); err != nil {
		t.Fatalf("AddMatchSignal: %v", err)
	}
	got := make(chan *dbus.Signal, 1)
	client.Signal(got)
	if err := fakeConn.Emit(ObjectPath, InterfaceName+".NotificationClosed", id, uint32(2)); err != nil {
		t.Fatalf("emit: %v", err)
	}
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for NotificationClosed")
	}
	for _, p := range []string{logo, image} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("copy %s not removed after close: %v", p, err)
		}
	}
}

func TestSignalsRelayOnlyOwnNotifications(t *testing.T) {
	_, fakeConn, client, _ := startProxy(t)

	if err := client.AddMatchSignal(dbus.WithMatchInterface(InterfaceName)); err != nil {
		t.Fatalf("AddMatchSignal: %v", err)
	}
	got := make(chan *dbus.Signal, 10)
	client.Signal(got)

	var id uint32
	if err := client.Object(BusName, ObjectPath).Call(InterfaceName+".Notify", 0,
		"app", uint32(0), "", "s", "b", []string{}, map[string]dbus.Variant{}, int32(-1),
	).Store(&id); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	emit := func(member string, body ...any) {
		if err := fakeConn.Emit(ObjectPath, InterfaceName+"."+member, body...); err != nil {
			t.Fatalf("emit %s: %v", member, err)
		}
	}
	emit("ActionInvoked", uint32(99), "default") // a host app's notification
	emit("ActivationToken", id, "token")
	emit("ActionInvoked", id, "default")
	emit("NotificationClosed", id, uint32(2))
	emit("ActionInvoked", id, "default") // after close, ID is forgotten

	want := []string{"ActivationToken", "ActionInvoked", "NotificationClosed"}
	for _, member := range want {
		select {
		case sig := <-got:
			if sig.Name != InterfaceName+"."+member {
				t.Fatalf("got signal %s, want %s", sig.Name, member)
			}
			if sig.Body[0] != id {
				t.Errorf("%s for id %v, want %d", member, sig.Body[0], id)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for %s", member)
		}
	}
	select {
	case sig := <-got:
		t.Errorf("unexpected signal %s %v", sig.Name, sig.Body)
	case <-time.After(200 * time.Millisecond):
	}
}

func notify(t *testing.T, client *dbus.Conn, replacesID uint32) uint32 {
	t.Helper()
	var id uint32
	if err := client.Object(BusName, ObjectPath).Call(InterfaceName+".Notify", 0,
		"app", replacesID, "", "s", "b", []string{}, map[string]dbus.Variant{}, int32(-1),
	).Store(&id); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	return id
}

func TestNotifyClearsUnownedReplacesID(t *testing.T) {
	fake, _, client, _ := startProxy(t)

	notify(t, client, 99) // a host app's notification
	fake.mu.Lock()
	got := fake.replacesID
	fake.mu.Unlock()
	if got != 0 {
		t.Errorf("host got replaces_id %d for an unowned ID, want 0", got)
	}

	id := notify(t, client, 0)
	notify(t, client, id)
	fake.mu.Lock()
	got = fake.replacesID
	fake.mu.Unlock()
	if got != id {
		t.Errorf("host got replaces_id %d, want own ID %d", got, id)
	}
}

func TestCloseNotificationIgnoresUnownedID(t *testing.T) {
	fake, _, client, _ := startProxy(t)
	obj := client.Object(BusName, ObjectPath)

	if err := obj.Call(InterfaceName+".CloseNotification", 0, uint32(99)).Err; err != nil {
		t.Fatalf("CloseNotification(99): %v", err)
	}
	id := notify(t, client, 0)
	if err := obj.Call(InterfaceName+".CloseNotification", 0, id).Err; err != nil {
		t.Fatalf("CloseNotification(%d): %v", id, err)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if !slices.Equal(fake.closed, []uint32{id}) {
		t.Errorf("host closed %v, want only own ID %d", fake.closed, id)
	}
}

func TestNotifyRateLimited(t *testing.T) {
	_, _, client, _ := startProxy(t)
	obj := client.Object(BusName, ObjectPath)

	var refused bool
	for range notifyBurst + 1 {
		err := obj.Call(InterfaceName+".Notify", 0,
			"app", uint32(0), "", "s", "b", []string{}, map[string]dbus.Variant{}, int32(-1),
		).Err
		if err != nil {
			refused = true
		}
	}
	if !refused {
		t.Errorf("%d calls in a row all passed, want the last refused", notifyBurst+1)
	}
}

func TestSignalBeforeNotifyReturns(t *testing.T) {
	fake, fakeConn, client, _ := startProxy(t)
	fake.mu.Lock()
	fake.conn = fakeConn
	fake.mu.Unlock()

	if err := client.AddMatchSignal(dbus.WithMatchInterface(InterfaceName)); err != nil {
		t.Fatalf("AddMatchSignal: %v", err)
	}
	got := make(chan *dbus.Signal, 1)
	client.Signal(got)

	id := notify(t, client, 0)
	select {
	case sig := <-got:
		if sig.Name != InterfaceName+".ActionInvoked" || sig.Body[0] != id || sig.Body[1] != "early" {
			t.Errorf("got %s %v, want the early ActionInvoked", sig.Name, sig.Body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("early signal was not relayed")
	}
}
