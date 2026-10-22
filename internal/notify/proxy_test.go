package notify

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/frostyard/intuneme/internal/busproxy"
	"github.com/frostyard/intuneme/internal/busproxy/busproxytest"
	"github.com/godbus/dbus/v5"
)

// fakeServer stands in for the host notification server.
type fakeServer struct {
	mu    sync.Mutex
	icon  string
	hints map[string]dbus.Variant
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
	return 7, nil
}

func (s *fakeServer) CloseNotification(id uint32) *dbus.Error { return nil }

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
func startProxy(t *testing.T) (*fakeServer, *dbus.Conn, *dbus.Conn) {
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
	paths := testRewriter("/proc/42/root/tmp/d/logo.png", "/proc/42/root/tmp/d/icon.png")
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- serve(ctx, proxyHost, proxyContainer, paths) }()
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
			return fake, fakeConn, client
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("proxy did not claim the bus name on the container bus")
	return nil, nil, nil
}

func TestGetCapabilitiesDropsBodyImages(t *testing.T) {
	_, _, client := startProxy(t)

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
	_, _, client := startProxy(t)

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

func TestNotifyRewritesPaths(t *testing.T) {
	fake, _, client := startProxy(t)

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
	defer fake.mu.Unlock()
	if fake.icon != "file:///proc/42/root/tmp/d/logo.png" {
		t.Errorf("host got app_icon %q", fake.icon)
	}
	if v := fake.hints["image-path"].Value(); v != "/proc/42/root/tmp/d/icon.png" {
		t.Errorf("host got image-path %v", v)
	}
}

func TestSignalsRelayOnlyOwnNotifications(t *testing.T) {
	_, fakeConn, client := startProxy(t)

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
