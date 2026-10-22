package notify

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/frostyard/intuneme/internal/broker"
	"github.com/frostyard/intuneme/internal/busproxy"
	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
)

const (
	BusName       = "org.freedesktop.Notifications"
	ObjectPath    = "/org/freedesktop/Notifications"
	InterfaceName = "org.freedesktop.Notifications"
)

// capabilityBodyImages is dropped from the host server's capabilities. With it
// Chromium embeds <img src="file:///tmp/..."> in the body, and those container
// paths are not rewritten.
const capabilityBodyImages = "body-images"

var errRateLimited = errors.New("too many notifications, try again later")

// relayedSignals are the host server signals forwarded into the container.
// ActionInvoked carries clicks, ActivationToken lets the clicked app take
// focus, and NotificationClosed ends a notification.
var relayedSignals = []string{"ActionInvoked", "ActivationToken", "NotificationClosed"}

// introspectXML describes the Desktop Notifications interface (spec 1.2).
const introspectXML = `<node>
  <interface name="` + InterfaceName + `">
    <method name="GetCapabilities">
      <arg name="capabilities" type="as" direction="out"/>
    </method>
    <method name="Notify">
      <arg name="app_name" type="s" direction="in"/>
      <arg name="replaces_id" type="u" direction="in"/>
      <arg name="app_icon" type="s" direction="in"/>
      <arg name="summary" type="s" direction="in"/>
      <arg name="body" type="s" direction="in"/>
      <arg name="actions" type="as" direction="in"/>
      <arg name="hints" type="a{sv}" direction="in"/>
      <arg name="expire_timeout" type="i" direction="in"/>
      <arg name="id" type="u" direction="out"/>
    </method>
    <method name="CloseNotification">
      <arg name="id" type="u" direction="in"/>
    </method>
    <method name="GetServerInformation">
      <arg name="name" type="s" direction="out"/>
      <arg name="vendor" type="s" direction="out"/>
      <arg name="version" type="s" direction="out"/>
      <arg name="spec_version" type="s" direction="out"/>
    </method>
    <signal name="NotificationClosed">
      <arg name="id" type="u"/>
      <arg name="reason" type="u"/>
    </signal>
    <signal name="ActionInvoked">
      <arg name="id" type="u"/>
      <arg name="action_key" type="s"/>
    </signal>
    <signal name="ActivationToken">
      <arg name="id" type="u"/>
      <arg name="activation_token" type="s"/>
    </signal>
  </interface>
  ` + introspect.IntrospectDataString + `
</node>`

// forwarder serves org.freedesktop.Notifications on the container bus and
// passes each call to the notification server on the host bus.
//
// Notification IDs are the host server's IDs, passed through unchanged. ids
// holds the IDs this proxy created. Only those IDs are relayed, replaced or
// closed, so the container cannot see or touch host apps' notifications.
type forwarder struct {
	host      *dbus.Conn
	container *dbus.Conn
	icons     *iconStore
	limiter   *rateLimiter
	now       func() time.Time

	// mu guards ids and early, and is held while signals are emitted on the
	// container bus, so signals for one ID keep their order.
	mu    sync.Mutex
	ids   *ownedIDs
	early earlySignals
}

func (f *forwarder) server() dbus.BusObject {
	return f.host.Object(BusName, ObjectPath)
}

func (f *forwarder) GetCapabilities() ([]string, *dbus.Error) {
	var caps []string
	if err := f.server().Call(InterfaceName+".GetCapabilities", 0).Store(&caps); err != nil {
		return nil, dbus.MakeFailedError(err)
	}
	return slices.DeleteFunc(caps, func(c string) bool { return c == capabilityBodyImages }), nil
}

func (f *forwarder) GetServerInformation() (string, string, string, string, *dbus.Error) {
	var name, vendor, version, spec string
	if err := f.server().Call(InterfaceName+".GetServerInformation", 0).Store(&name, &vendor, &version, &spec); err != nil {
		return "", "", "", "", dbus.MakeFailedError(err)
	}
	return name, vendor, version, spec, nil
}

func (f *forwarder) Notify(appName string, replacesID uint32, appIcon, summary, body string,
	actions []string, hints map[string]dbus.Variant, expireTimeout int32) (uint32, *dbus.Error) {
	if !f.limiter.allow() {
		return 0, dbus.MakeFailedError(errRateLimited)
	}
	if !f.isOwned(replacesID) {
		replacesID = 0
	}

	copies := f.icons.begin()
	icon := copies.appIcon(appIcon)
	hints = copies.hints(filterHints(hints))

	var id uint32
	err := f.server().Call(InterfaceName+".Notify", 0,
		appName, replacesID, icon, summary, body,
		actions, hints, expireTimeout,
	).Store(&id)
	if err != nil {
		f.icons.remove(copies.files)
		return 0, dbus.MakeFailedError(err)
	}

	f.mu.Lock()
	stale := f.ids.add(id, copies.files)
	for _, sig := range f.early.take(id, f.now()) {
		stale = append(stale, f.emit(id, sig)...)
	}
	f.mu.Unlock()
	f.icons.remove(stale)
	return id, nil
}

// CloseNotification ignores an ID the proxy did not create.
func (f *forwarder) CloseNotification(id uint32) *dbus.Error {
	if !f.isOwned(id) {
		return nil
	}
	if err := f.server().Call(InterfaceName+".CloseNotification", 0, id).Err; err != nil {
		return dbus.MakeFailedError(err)
	}
	return nil
}

func (f *forwarder) isOwned(id uint32) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ids.has(id)
}

// emit re-emits sig for an owned id on the container bus. A
// NotificationClosed signal also forgets the ID, and emit returns its icon
// copies for removal. f.mu must be held.
func (f *forwarder) emit(id uint32, sig *dbus.Signal) []string {
	if err := f.container.Emit(ObjectPath, sig.Name, sig.Body...); err != nil {
		log.Printf("emit %s for notification %d: %v", sig.Name, id, err)
	}
	if sig.Name == InterfaceName+".NotificationClosed" {
		return f.ids.remove(id)
	}
	return nil
}

// handle relays one host signal. A signal for an ID the proxy does not own
// is buffered for a short time, in case a Notify call for that ID has not
// returned yet. Signals for host apps' notifications expire from the buffer
// and are never relayed.
func (f *forwarder) handle(id uint32, sig *dbus.Signal) {
	f.mu.Lock()
	var stale []string
	if f.ids.has(id) {
		stale = f.emit(id, sig)
	} else {
		f.early.add(id, sig, f.now())
	}
	f.mu.Unlock()
	f.icons.remove(stale)
}

// relay re-emits the host server's signals for this proxy's notifications on
// the container bus. It returns when ch closes.
func (f *forwarder) relay(ch <-chan *dbus.Signal) {
	for sig := range ch {
		if sig.Path != ObjectPath || len(sig.Body) == 0 {
			continue
		}
		member, ok := strings.CutPrefix(sig.Name, InterfaceName+".")
		if !ok || !slices.Contains(relayedSignals, member) {
			continue
		}
		id, ok := sig.Body[0].(uint32)
		if !ok {
			continue
		}
		f.handle(id, sig)
	}
}

// serve exports the forwarder on the container connection, claims the
// notifications bus name there, and relays host signals until ctx is
// cancelled or either connection closes.
func serve(ctx context.Context, host, container *dbus.Conn, icons *iconStore) error {
	fwd := &forwarder{
		host:      host,
		container: container,
		icons:     icons,
		limiter:   newRateLimiter(notifyBurst, notifyRefill, time.Now),
		now:       time.Now,
		ids:       newOwnedIDs(),
	}

	if err := container.Export(fwd, ObjectPath, InterfaceName); err != nil {
		return fmt.Errorf("export forwarder: %w", err)
	}
	if err := container.Export(introspect.Introspectable(introspectXML), ObjectPath, "org.freedesktop.DBus.Introspectable"); err != nil {
		return fmt.Errorf("export introspectable: %w", err)
	}

	// Match on the well-known name so the rule follows a restarted host
	// notification server.
	if err := host.AddMatchSignal(
		dbus.WithMatchSender(BusName),
		dbus.WithMatchObjectPath(ObjectPath),
		dbus.WithMatchInterface(InterfaceName),
	); err != nil {
		return fmt.Errorf("subscribe to host notification signals: %w", err)
	}
	signals := make(chan *dbus.Signal, 16)
	host.Signal(signals)
	defer host.RemoveSignal(signals)
	go fwd.relay(signals)

	reply, err := container.RequestName(BusName, dbus.NameFlagDoNotQueue)
	if err != nil {
		return fmt.Errorf("request bus name %s: %w", BusName, err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return fmt.Errorf("bus name %s already owned on the container bus", BusName)
	}

	select {
	case <-ctx.Done():
		return nil
	case <-host.Context().Done():
		return fmt.Errorf("host session bus connection closed")
	case <-container.Context().Done():
		return fmt.Errorf("container session bus connection closed")
	}
}

// Run connects to the container's session bus and the host session bus and
// forwards notifications from the container to the host until ctx is
// cancelled.
func Run(ctx context.Context, root string) error {
	socket := broker.SessionBusSocketPath(root)
	container, pid, err := busproxy.Dial(socket)
	if err != nil {
		return err
	}
	defer func() { _ = container.Close() }()

	host, err := dbus.ConnectSessionBus()
	if err != nil {
		return fmt.Errorf("connect host session bus: %w", err)
	}
	defer func() { _ = host.Close() }()

	dir, err := iconDir()
	if err != nil {
		return err
	}
	icons, err := newIconStore(pid, dir)
	if err != nil {
		return err
	}
	defer icons.close()

	log.Printf("Notify proxy running: serving %s on %s, container files via /proc/%d/root", BusName, socket, pid)
	err = serve(ctx, host, container, icons)
	log.Println("Notify proxy shutting down")
	return err
}
