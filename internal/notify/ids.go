package notify

import (
	"slices"
	"time"

	"github.com/godbus/dbus/v5"
)

// Limits on the proxy's notification state. A host server that never sends
// NotificationClosed must not make the state grow without bound.
const (
	// maxOwnedIDs is the number of notification IDs the proxy remembers.
	// When it is full, the oldest ID is forgotten and its icon copies removed.
	maxOwnedIDs = 256
	// maxEarlySignals and earlySignalTTL bound the buffer for signals that
	// arrive before the Notify call that created their ID has returned.
	maxEarlySignals = 32
	earlySignalTTL  = 2 * time.Second
)

// ownedIDs is the set of notification IDs the proxy created, in creation
// order, with the icon copies made for each.
type ownedIDs struct {
	files map[uint32][]string
	order []uint32
}

func newOwnedIDs() *ownedIDs {
	return &ownedIDs{files: map[uint32][]string{}}
}

func (o *ownedIDs) has(id uint32) bool {
	_, ok := o.files[id]
	return ok
}

// add records id with its icon copies and returns the copies that are no
// longer needed: those of an earlier notification with the same ID, and those
// of an evicted ID.
func (o *ownedIDs) add(id uint32, files []string) []string {
	stale, ok := o.files[id]
	if ok {
		o.order = slices.DeleteFunc(o.order, func(x uint32) bool { return x == id })
	}
	for len(o.order) >= maxOwnedIDs {
		oldest := o.order[0]
		o.order = o.order[1:]
		stale = append(stale, o.files[oldest]...)
		delete(o.files, oldest)
	}
	o.files[id] = files
	o.order = append(o.order, id)
	return stale
}

// remove forgets id and returns its icon copies.
func (o *ownedIDs) remove(id uint32) []string {
	files, ok := o.files[id]
	if !ok {
		return nil
	}
	delete(o.files, id)
	o.order = slices.DeleteFunc(o.order, func(x uint32) bool { return x == id })
	return files
}

// earlySignal is a host signal for an ID the proxy does not own yet.
type earlySignal struct {
	id  uint32
	sig *dbus.Signal
	at  time.Time
}

// earlySignals buffers host signals that may belong to a Notify call that has
// not returned yet. The host server can emit a signal for a new ID before the
// proxy reads the Notify reply.
type earlySignals struct {
	buf []earlySignal
}

// expire drops entries older than earlySignalTTL.
func (e *earlySignals) expire(now time.Time) {
	e.buf = slices.DeleteFunc(e.buf, func(s earlySignal) bool { return now.Sub(s.at) > earlySignalTTL })
}

// add buffers a signal and drops the oldest entry when the buffer is full.
func (e *earlySignals) add(id uint32, sig *dbus.Signal, now time.Time) {
	e.expire(now)
	if len(e.buf) >= maxEarlySignals {
		e.buf = e.buf[1:]
	}
	e.buf = append(e.buf, earlySignal{id: id, sig: sig, at: now})
}

// take removes and returns the buffered signals for id, oldest first.
func (e *earlySignals) take(id uint32, now time.Time) []*dbus.Signal {
	e.expire(now)
	var out []*dbus.Signal
	e.buf = slices.DeleteFunc(e.buf, func(s earlySignal) bool {
		if s.id != id {
			return false
		}
		out = append(out, s.sig)
		return true
	})
	return out
}
