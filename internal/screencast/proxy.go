// Package screencast serves the ScreenCast portal on the container's session
// bus and forwards it to the xdg-desktop-portal on the host session bus, so
// apps in the container (Edge) can share host screens and windows.
package screencast

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"syscall"

	"github.com/frostyard/intuneme/internal/broker"
	"github.com/frostyard/intuneme/internal/busproxy"
	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
)

const (
	BusName             = "org.freedesktop.portal.Desktop"
	ObjectPath          = dbus.ObjectPath("/org/freedesktop/portal/desktop")
	ScreenCastInterface = "org.freedesktop.portal.ScreenCast"
	RequestInterface    = "org.freedesktop.portal.Request"
	SessionInterface    = "org.freedesktop.portal.Session"

	requestPrefix = "/org/freedesktop/portal/desktop/request/"
	sessionPrefix = "/org/freedesktop/portal/desktop/session/"
)

// maxSessions is the number of ScreenCast sessions open at a time. Each one
// can show a picker on the host, so a container process must not open many.
const maxSessions = 4

// tokenPattern is the portal spec's form for handle_token and
// session_handle_token. Tokens become object path elements.
var tokenPattern = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`)

var (
	errNoSession    = errors.New("no such session")
	errTooMany      = fmt.Errorf("at most %d screencast sessions can be open", maxSessions)
	errBadToken     = errors.New("invalid handle token")
	errPathInUse    = errors.New("object path already in use")
	errNotSupported = errors.New("only org.freedesktop.portal.ScreenCast is available")
)

// introspectXML describes the object the proxy serves. Only the ScreenCast
// interface is present: the proxy replaces the container's own portal, and
// every other portal interface is missing while it runs.
const introspectXML = `<node>
  <interface name="` + ScreenCastInterface + `">
    <method name="CreateSession">
      <arg type="a{sv}" name="options" direction="in"/>
      <arg type="o" name="handle" direction="out"/>
    </method>
    <method name="SelectSources">
      <arg type="o" name="session_handle" direction="in"/>
      <arg type="a{sv}" name="options" direction="in"/>
      <arg type="o" name="handle" direction="out"/>
    </method>
    <method name="Start">
      <arg type="o" name="session_handle" direction="in"/>
      <arg type="s" name="parent_window" direction="in"/>
      <arg type="a{sv}" name="options" direction="in"/>
      <arg type="o" name="handle" direction="out"/>
    </method>
    <method name="OpenPipeWireRemote">
      <arg type="o" name="session_handle" direction="in"/>
      <arg type="a{sv}" name="options" direction="in"/>
      <arg type="h" name="fd" direction="out"/>
    </method>
    <property name="AvailableSourceTypes" type="u" access="read"/>
    <property name="AvailableCursorModes" type="u" access="read"/>
    <property name="version" type="u" access="read"/>
  </interface>
  <interface name="org.freedesktop.DBus.Properties">
    <method name="Get">
      <arg type="s" name="interface_name" direction="in"/>
      <arg type="s" name="property_name" direction="in"/>
      <arg type="v" name="value" direction="out"/>
    </method>
    <method name="GetAll">
      <arg type="s" name="interface_name" direction="in"/>
      <arg type="a{sv}" name="props" direction="out"/>
    </method>
  </interface>
  ` + introspect.IntrospectDataString + `
</node>`

// senderElement turns a unique bus name into the object path element the
// portal uses for it: ":1.42" becomes "1_42".
func senderElement(uniqueName string) string {
	return strings.ReplaceAll(strings.TrimPrefix(uniqueName, ":"), ".", "_")
}

// session is one ScreenCast session. The container and the host see it at
// different object paths, because the portal builds the path from the
// caller's unique name, and the caller on the host is the proxy.
type session struct {
	caller        string
	containerPath dbus.ObjectPath
	hostPath      dbus.ObjectPath
	// fds are the PipeWire fds the host returned for this session. The
	// proxy keeps its copy until the session ends.
	fds []int
}

// requestKind says how a Response is rewritten.
type requestKind int

const (
	requestCreateSession requestKind = iota
	requestSelectSources
	requestStart
)

// request is one pending portal Request.
type request struct {
	kind          requestKind
	caller        string
	containerPath dbus.ObjectPath
	hostPath      dbus.ObjectPath
	session       *session
}

// proxy holds the state shared by the exported objects and the signal loops.
type proxy struct {
	host      *dbus.Conn
	container *dbus.Conn
	// hostSender is the proxy's own host unique name as a path element.
	hostSender string

	mu sync.Mutex
	// next numbers the proxy's own host tokens.
	next uint64
	// requests and sessions are keyed by host path. containerPaths holds
	// every container path in use, so two objects never share one.
	requests       map[dbus.ObjectPath]*request
	sessions       map[dbus.ObjectPath]*session
	containerPaths map[dbus.ObjectPath]bool
}

func newProxy(host, container *dbus.Conn) *proxy {
	return &proxy{
		host:           host,
		container:      container,
		hostSender:     senderElement(host.Names()[0]),
		requests:       map[dbus.ObjectPath]*request{},
		sessions:       map[dbus.ObjectPath]*session{},
		containerPaths: map[dbus.ObjectPath]bool{},
	}
}

func (p *proxy) portal() dbus.BusObject {
	return p.host.Object(BusName, ObjectPath)
}

// containerToken returns the token the caller asked for in options[key], or
// a generated one.
func (p *proxy) containerToken(options map[string]dbus.Variant, key string) (string, error) {
	v, ok := options[key]
	if !ok {
		p.next++
		return fmt.Sprintf("intuneme_%d", p.next), nil
	}
	s, ok := v.Value().(string)
	if !ok || !tokenPattern.MatchString(s) {
		return "", errBadToken
	}
	return s, nil
}

// hostToken returns a fresh token for a call to the host portal.
func (p *proxy) hostToken() string {
	p.next++
	return fmt.Sprintf("intuneme_%d", p.next)
}

// newRequest registers a request for caller and exports its Request object
// on the container bus. p.mu must be held.
func (p *proxy) newRequest(kind requestKind, caller string, options map[string]dbus.Variant, s *session) (*request, string, error) {
	token, err := p.containerToken(options, "handle_token")
	if err != nil {
		return nil, "", err
	}
	containerPath := dbus.ObjectPath(requestPrefix + senderElement(caller) + "/" + token)
	if p.containerPaths[containerPath] {
		return nil, "", errPathInUse
	}
	hostToken := p.hostToken()
	r := &request{
		kind:          kind,
		caller:        caller,
		containerPath: containerPath,
		hostPath:      dbus.ObjectPath(requestPrefix + p.hostSender + "/" + hostToken),
		session:       s,
	}
	if err := p.container.Export(&requestObject{p: p, r: r}, containerPath, RequestInterface); err != nil {
		return nil, "", err
	}
	p.requests[r.hostPath] = r
	p.containerPaths[containerPath] = true
	return r, hostToken, nil
}

// dropRequest forgets r. p.mu must be held.
func (p *proxy) dropRequest(r *request) {
	if p.requests[r.hostPath] != r {
		return
	}
	delete(p.requests, r.hostPath)
	delete(p.containerPaths, r.containerPath)
	_ = p.container.Export(nil, r.containerPath, RequestInterface)
}

// rekeyRequest moves r to the host path the portal returned, in case it
// differs from the one the proxy computed. p.mu must be held.
func (p *proxy) rekeyRequest(r *request, hostPath dbus.ObjectPath) {
	if hostPath == r.hostPath || p.requests[r.hostPath] != r {
		return
	}
	delete(p.requests, r.hostPath)
	r.hostPath = hostPath
	p.requests[hostPath] = r
}

// dropSession forgets s, its requests and its fds. p.mu must be held.
func (p *proxy) dropSession(s *session) {
	if p.sessions[s.hostPath] != s {
		return
	}
	delete(p.sessions, s.hostPath)
	delete(p.containerPaths, s.containerPath)
	_ = p.container.Export(nil, s.containerPath, SessionInterface)
	for _, fd := range s.fds {
		_ = syscall.Close(fd)
	}
	s.fds = nil
	for _, r := range p.requests {
		if r.session == s {
			p.dropRequest(r)
		}
	}
}

// callerSession returns the session at containerPath if caller owns it.
// p.mu must be held.
func (p *proxy) callerSession(caller string, containerPath dbus.ObjectPath) (*session, error) {
	for _, s := range p.sessions {
		if s.containerPath == containerPath && s.caller == caller {
			return s, nil
		}
	}
	return nil, errNoSession
}

// screenCast is the org.freedesktop.portal.ScreenCast object.
type screenCast struct{ p *proxy }

func (sc *screenCast) CreateSession(sender dbus.Sender, options map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	p := sc.p
	caller := string(sender)

	p.mu.Lock()
	if len(p.sessions) >= maxSessions {
		p.mu.Unlock()
		return "", dbus.MakeFailedError(errTooMany)
	}
	sessionToken, err := p.containerToken(options, "session_handle_token")
	if err != nil {
		p.mu.Unlock()
		return "", dbus.MakeFailedError(err)
	}
	s := &session{
		caller:        caller,
		containerPath: dbus.ObjectPath(sessionPrefix + senderElement(caller) + "/" + sessionToken),
	}
	if p.containerPaths[s.containerPath] {
		p.mu.Unlock()
		return "", dbus.MakeFailedError(errPathInUse)
	}
	hostSessionToken := p.hostToken()
	s.hostPath = dbus.ObjectPath(sessionPrefix + p.hostSender + "/" + hostSessionToken)
	r, hostToken, err := p.newRequest(requestCreateSession, caller, options, s)
	if err != nil {
		p.mu.Unlock()
		return "", dbus.MakeFailedError(err)
	}
	p.sessions[s.hostPath] = s
	p.containerPaths[s.containerPath] = true
	p.mu.Unlock()

	var hostPath dbus.ObjectPath
	err = p.portal().Call(ScreenCastInterface+".CreateSession", 0, map[string]dbus.Variant{
		"handle_token":         dbus.MakeVariant(hostToken),
		"session_handle_token": dbus.MakeVariant(hostSessionToken),
	}).Store(&hostPath)

	p.mu.Lock()
	defer p.mu.Unlock()
	if err != nil {
		p.dropSession(s)
		p.dropRequest(r)
		return "", dbus.MakeFailedError(err)
	}
	p.rekeyRequest(r, hostPath)
	return r.containerPath, nil
}

// sessionCall forwards a call on an existing session that answers with a
// Request.
func (sc *screenCast) sessionCall(kind requestKind, method string, caller string, sessionPath dbus.ObjectPath,
	options map[string]dbus.Variant, hostArgs func(hostSession dbus.ObjectPath, hostToken string) []any) (dbus.ObjectPath, *dbus.Error) {
	p := sc.p

	p.mu.Lock()
	s, err := p.callerSession(caller, sessionPath)
	if err != nil {
		p.mu.Unlock()
		return "", dbus.MakeFailedError(err)
	}
	r, hostToken, err := p.newRequest(kind, caller, options, s)
	if err != nil {
		p.mu.Unlock()
		return "", dbus.MakeFailedError(err)
	}
	hostSession := s.hostPath
	p.mu.Unlock()

	var hostPath dbus.ObjectPath
	err = p.portal().Call(ScreenCastInterface+"."+method, 0, hostArgs(hostSession, hostToken)...).Store(&hostPath)

	p.mu.Lock()
	defer p.mu.Unlock()
	if err != nil {
		p.dropRequest(r)
		return "", dbus.MakeFailedError(err)
	}
	p.rekeyRequest(r, hostPath)
	return r.containerPath, nil
}

func (sc *screenCast) SelectSources(sender dbus.Sender, sessionPath dbus.ObjectPath, options map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	return sc.sessionCall(requestSelectSources, "SelectSources", string(sender), sessionPath, options,
		func(hostSession dbus.ObjectPath, hostToken string) []any {
			// persist_mode and restore_token are dropped, so every capture
			// shows the host picker.
			opts := map[string]dbus.Variant{"handle_token": dbus.MakeVariant(hostToken)}
			for key, sig := range map[string]string{"types": "u", "multiple": "b", "cursor_mode": "u"} {
				if v, ok := options[key]; ok && v.Signature().String() == sig {
					opts[key] = v
				}
			}
			return []any{hostSession, opts}
		})
}

func (sc *screenCast) Start(sender dbus.Sender, sessionPath dbus.ObjectPath, parentWindow string, options map[string]dbus.Variant) (dbus.ObjectPath, *dbus.Error) {
	return sc.sessionCall(requestStart, "Start", string(sender), sessionPath, options,
		func(hostSession dbus.ObjectPath, hostToken string) []any {
			// A container window handle means nothing to the host portal.
			return []any{hostSession, "", map[string]dbus.Variant{"handle_token": dbus.MakeVariant(hostToken)}}
		})
}

func (sc *screenCast) OpenPipeWireRemote(sender dbus.Sender, sessionPath dbus.ObjectPath, options map[string]dbus.Variant) (dbus.UnixFD, *dbus.Error) {
	p := sc.p

	p.mu.Lock()
	s, err := p.callerSession(string(sender), sessionPath)
	if err != nil {
		p.mu.Unlock()
		return -1, dbus.MakeFailedError(err)
	}
	hostSession := s.hostPath
	p.mu.Unlock()

	var fd dbus.UnixFD
	if err := p.portal().Call(ScreenCastInterface+".OpenPipeWireRemote", 0,
		hostSession, map[string]dbus.Variant{}).Store(&fd); err != nil {
		return -1, dbus.MakeFailedError(err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sessions[hostSession] != s {
		_ = syscall.Close(int(fd))
		return -1, dbus.MakeFailedError(errNoSession)
	}
	// godbus sends the fd but does not close it. The proxy keeps it with
	// the session and closes it when the session ends.
	s.fds = append(s.fds, int(fd))
	return fd, nil
}

// properties serves org.freedesktop.DBus.Properties for the ScreenCast
// interface by reading the host portal's values.
type properties struct{ p *proxy }

func (pr *properties) Get(iface, name string) (dbus.Variant, *dbus.Error) {
	if iface != ScreenCastInterface {
		return dbus.Variant{}, dbus.MakeFailedError(errNotSupported)
	}
	v, err := pr.p.portal().GetProperty(ScreenCastInterface + "." + name)
	if err != nil {
		return dbus.Variant{}, dbus.MakeFailedError(err)
	}
	return v, nil
}

func (pr *properties) GetAll(iface string) (map[string]dbus.Variant, *dbus.Error) {
	if iface != ScreenCastInterface {
		return nil, dbus.MakeFailedError(errNotSupported)
	}
	var all map[string]dbus.Variant
	if err := pr.p.portal().Call("org.freedesktop.DBus.Properties.GetAll", 0, ScreenCastInterface).Store(&all); err != nil {
		return nil, dbus.MakeFailedError(err)
	}
	return all, nil
}

func (pr *properties) Set(iface, name string, value dbus.Variant) *dbus.Error {
	return dbus.MakeFailedError(errors.New("properties are read-only"))
}

// requestObject is a Request on the container bus.
type requestObject struct {
	p *proxy
	r *request
}

func (o *requestObject) Close(sender dbus.Sender) *dbus.Error {
	p := o.p
	p.mu.Lock()
	if string(sender) != o.r.caller || p.requests[o.r.hostPath] != o.r {
		p.mu.Unlock()
		return dbus.MakeFailedError(errors.New("no such request"))
	}
	hostPath := o.r.hostPath
	p.dropRequest(o.r)
	if o.r.kind == requestCreateSession {
		p.dropSession(o.r.session)
	}
	p.mu.Unlock()

	if err := p.host.Object(BusName, hostPath).Call(RequestInterface+".Close", 0).Err; err != nil {
		log.Printf("close host request %s: %v", hostPath, err)
	}
	return nil
}

// sessionObject is a Session on the container bus.
type sessionObject struct {
	p *proxy
	s *session
}

func (o *sessionObject) Close(sender dbus.Sender) *dbus.Error {
	p := o.p
	p.mu.Lock()
	if string(sender) != o.s.caller || p.sessions[o.s.hostPath] != o.s {
		p.mu.Unlock()
		return dbus.MakeFailedError(errNoSession)
	}
	p.dropSession(o.s)
	p.mu.Unlock()

	p.closeHostSession(o.s.hostPath)
	return nil
}

func (p *proxy) closeHostSession(hostPath dbus.ObjectPath) {
	if err := p.host.Object(BusName, hostPath).Call(SessionInterface+".Close", 0).Err; err != nil {
		log.Printf("close host session %s: %v", hostPath, err)
	}
}

// emitTo sends a signal on the container bus addressed to caller, as the real
// portal does. WebRTC subscribes to Response with
// G_DBUS_SIGNAL_FLAGS_NO_MATCH_RULE, so it adds no match rule, and the bus
// would not deliver a broadcast signal to it.
func (p *proxy) emitTo(caller string, path dbus.ObjectPath, iface, member string, values ...any) error {
	msg := &dbus.Message{
		Type: dbus.TypeSignal,
		Headers: map[dbus.HeaderField]dbus.Variant{
			dbus.FieldPath:        dbus.MakeVariant(path),
			dbus.FieldInterface:   dbus.MakeVariant(iface),
			dbus.FieldMember:      dbus.MakeVariant(member),
			dbus.FieldDestination: dbus.MakeVariant(caller),
			dbus.FieldSignature:   dbus.MakeVariant(dbus.SignatureOf(values...)),
		},
		Body: values,
	}
	return p.container.Send(msg, nil).Err
}

// onResponse relays a host Request.Response to the container caller.
func (p *proxy) onResponse(sig *dbus.Signal) {
	if len(sig.Body) != 2 {
		return
	}
	code, ok1 := sig.Body[0].(uint32)
	results, ok2 := sig.Body[1].(map[string]dbus.Variant)
	if !ok1 || !ok2 {
		return
	}

	p.mu.Lock()
	r, ok := p.requests[sig.Path]
	if !ok {
		p.mu.Unlock()
		return
	}
	p.dropRequest(r)
	out := map[string]dbus.Variant{}
	switch r.kind {
	case requestCreateSession:
		if code == 0 {
			out["session_handle"] = dbus.MakeVariant(string(r.session.containerPath))
			if err := p.container.Export(&sessionObject{p: p, s: r.session}, r.session.containerPath, SessionInterface); err != nil {
				log.Printf("export session %s: %v", r.session.containerPath, err)
			}
		} else {
			p.dropSession(r.session)
		}
	case requestStart:
		// restore_token and persist_mode are not passed: every capture
		// shows the host picker.
		if streams, ok := results["streams"]; ok {
			out["streams"] = dbus.MakeVariant(convertStreams(streams))
		}
	}
	p.mu.Unlock()

	if err := p.emitTo(r.caller, r.containerPath, RequestInterface, "Response", code, out); err != nil {
		log.Printf("emit Response on %s: %v", r.containerPath, err)
	}
}

// stream is one entry of the Start results' "streams" value, a(ua{sv}).
type stream struct {
	NodeID uint32
	Props  map[string]dbus.Variant
}

// point is the (ii) type of the stream position and size properties.
type point struct {
	X, Y int32
}

// convertStreams turns the decoded "streams" value back into typed Go values.
// godbus decodes structs into []any, and cannot encode those as structs
// again. Only the stream properties the portal spec defines are kept.
func convertStreams(v dbus.Variant) []stream {
	raw, ok := v.Value().([][]any)
	if !ok {
		return nil
	}
	var out []stream
	for _, entry := range raw {
		if len(entry) != 2 {
			continue
		}
		id, ok1 := entry[0].(uint32)
		props, ok2 := entry[1].(map[string]dbus.Variant)
		if !ok1 || !ok2 {
			continue
		}
		s := stream{NodeID: id, Props: map[string]dbus.Variant{}}
		for key, prop := range props {
			switch key {
			case "source_type":
				if u, ok := prop.Value().(uint32); ok {
					s.Props[key] = dbus.MakeVariant(u)
				}
			case "id", "mapping_id":
				if str, ok := prop.Value().(string); ok {
					s.Props[key] = dbus.MakeVariant(str)
				}
			case "position", "size":
				if xy, ok := prop.Value().([]any); ok && len(xy) == 2 {
					x, okx := xy[0].(int32)
					y, oky := xy[1].(int32)
					if okx && oky {
						s.Props[key] = dbus.MakeVariant(point{X: x, Y: y})
					}
				}
			}
		}
		out = append(out, s)
	}
	return out
}

// onSessionClosed relays a host Session.Closed to the container caller.
func (p *proxy) onSessionClosed(sig *dbus.Signal) {
	p.mu.Lock()
	s, ok := p.sessions[sig.Path]
	if ok {
		p.dropSession(s)
	}
	p.mu.Unlock()
	if !ok {
		return
	}
	details := map[string]dbus.Variant{}
	if len(sig.Body) == 1 {
		if d, ok := sig.Body[0].(map[string]dbus.Variant); ok {
			details = d
		}
	}
	if err := p.emitTo(s.caller, s.containerPath, SessionInterface, "Closed", details); err != nil {
		log.Printf("emit Closed on %s: %v", s.containerPath, err)
	}
}

// onCallerGone closes the host sessions of a container caller that left the
// bus. The host portal cannot see it leave: on the host, the caller of every
// session is the proxy.
func (p *proxy) onCallerGone(caller string) {
	p.mu.Lock()
	var closed []dbus.ObjectPath
	for _, s := range p.sessions {
		if s.caller == caller {
			closed = append(closed, s.hostPath)
			p.dropSession(s)
		}
	}
	for _, r := range p.requests {
		if r.caller == caller {
			p.dropRequest(r)
		}
	}
	p.mu.Unlock()
	for _, hostPath := range closed {
		p.closeHostSession(hostPath)
	}
}

// closeAll closes every host session, when the proxy stops.
func (p *proxy) closeAll() {
	p.mu.Lock()
	var closed []dbus.ObjectPath
	for _, s := range p.sessions {
		closed = append(closed, s.hostPath)
		p.dropSession(s)
	}
	p.mu.Unlock()
	for _, hostPath := range closed {
		p.closeHostSession(hostPath)
	}
}

// hostSignals handles host portal signals until ch closes.
func (p *proxy) hostSignals(ch <-chan *dbus.Signal) {
	for sig := range ch {
		switch sig.Name {
		case RequestInterface + ".Response":
			p.onResponse(sig)
		case SessionInterface + ".Closed":
			p.onSessionClosed(sig)
		}
	}
}

// containerSignals handles container bus signals until ch closes. It reports
// on lost when the proxy loses the portal name.
func (p *proxy) containerSignals(ch <-chan *dbus.Signal, lost chan<- struct{}) {
	for sig := range ch {
		switch sig.Name {
		case "org.freedesktop.DBus.NameOwnerChanged":
			if len(sig.Body) != 3 {
				continue
			}
			name, _ := sig.Body[0].(string)
			newOwner, _ := sig.Body[2].(string)
			if strings.HasPrefix(name, ":") && newOwner == "" {
				p.onCallerGone(name)
			}
		case "org.freedesktop.DBus.NameLost":
			if len(sig.Body) == 1 && sig.Body[0] == BusName {
				select {
				case lost <- struct{}{}:
				default:
				}
			}
		}
	}
}

// serve exports the portal on the container connection, takes over the
// portal name there, and forwards ScreenCast until ctx is cancelled or either
// connection closes. ready, when not nil, runs once the proxy owns the name.
func serve(ctx context.Context, host, container *dbus.Conn, ready func() error) error {
	p := newProxy(host, container)
	defer p.closeAll()

	if err := container.Export(&screenCast{p: p}, ObjectPath, ScreenCastInterface); err != nil {
		return fmt.Errorf("export ScreenCast: %w", err)
	}
	if err := container.Export(&properties{p: p}, ObjectPath, "org.freedesktop.DBus.Properties"); err != nil {
		return fmt.Errorf("export properties: %w", err)
	}
	if err := container.Export(introspect.Introspectable(introspectXML), ObjectPath, "org.freedesktop.DBus.Introspectable"); err != nil {
		return fmt.Errorf("export introspectable: %w", err)
	}

	// The portal emits Response on the request path it built from the
	// proxy's unique name. Closed comes on the session path.
	if err := host.AddMatchSignal(
		dbus.WithMatchSender(BusName),
		dbus.WithMatchInterface(RequestInterface),
		dbus.WithMatchMember("Response"),
		dbus.WithMatchPathNamespace(dbus.ObjectPath(requestPrefix+p.hostSender)),
	); err != nil {
		return fmt.Errorf("subscribe to host Response signals: %w", err)
	}
	if err := host.AddMatchSignal(
		dbus.WithMatchSender(BusName),
		dbus.WithMatchInterface(SessionInterface),
		dbus.WithMatchMember("Closed"),
		dbus.WithMatchPathNamespace(dbus.ObjectPath(sessionPrefix+p.hostSender)),
	); err != nil {
		return fmt.Errorf("subscribe to host Closed signals: %w", err)
	}
	hostCh := make(chan *dbus.Signal, 32)
	host.Signal(hostCh)
	defer host.RemoveSignal(hostCh)
	go p.hostSignals(hostCh)

	if err := container.AddMatchSignal(
		dbus.WithMatchSender("org.freedesktop.DBus"),
		dbus.WithMatchInterface("org.freedesktop.DBus"),
		dbus.WithMatchMember("NameOwnerChanged"),
	); err != nil {
		return fmt.Errorf("subscribe to container NameOwnerChanged: %w", err)
	}
	containerCh := make(chan *dbus.Signal, 32)
	container.Signal(containerCh)
	defer container.RemoveSignal(containerCh)
	lost := make(chan struct{}, 1)
	go p.containerSignals(containerCh, lost)

	// The container's xdg-desktop-portal owns the name with
	// ALLOW_REPLACEMENT and quits when it loses it. The proxy does not allow
	// replacement, so while it runs the container bus does not start that
	// portal again.
	reply, err := container.RequestName(BusName, dbus.NameFlagReplaceExisting|dbus.NameFlagDoNotQueue)
	if err != nil {
		return fmt.Errorf("request bus name %s: %w", BusName, err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return fmt.Errorf("bus name %s is owned on the container bus by a process that does not allow replacement", BusName)
	}
	if ready != nil {
		if err := ready(); err != nil {
			return err
		}
	}

	select {
	case <-ctx.Done():
		return nil
	case <-lost:
		return fmt.Errorf("lost bus name %s on the container bus", BusName)
	case <-host.Context().Done():
		return fmt.Errorf("host session bus connection closed")
	case <-container.Context().Done():
		return fmt.Errorf("container session bus connection closed")
	}
}

// Run connects to the container's session bus and the host session bus and
// forwards the ScreenCast portal until ctx is cancelled. ready runs once the
// proxy owns the portal name.
func Run(ctx context.Context, root string, ready func() error) error {
	socket := broker.SessionBusSocketPath(root)
	container, _, err := busproxy.Dial(socket)
	if err != nil {
		return err
	}
	defer func() { _ = container.Close() }()

	host, err := dbus.ConnectSessionBus()
	if err != nil {
		return fmt.Errorf("connect host session bus: %w", err)
	}
	defer func() { _ = host.Close() }()

	log.Printf("Screencast proxy running: serving %s on %s", BusName, socket)
	err = serve(ctx, host, container, ready)
	log.Println("Screencast proxy shutting down")
	return err
}
