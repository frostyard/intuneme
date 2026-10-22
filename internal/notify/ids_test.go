package notify

import (
	"slices"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

func TestOwnedIDsEvictsOldest(t *testing.T) {
	o := newOwnedIDs()
	for id := range uint32(maxOwnedIDs) {
		if stale := o.add(id, []string{"f"}); len(stale) != 0 {
			t.Fatalf("add(%d) returned stale %v before the cap", id, stale)
		}
	}
	// Re-adding ID 0 makes it the newest and returns its old copies.
	if stale := o.add(0, []string{"new0"}); !slices.Equal(stale, []string{"f"}) {
		t.Fatalf("re-add stale = %v, want the old copies", stale)
	}
	// The table is full, so the next add evicts ID 1, now the oldest.
	stale := o.add(maxOwnedIDs, nil)
	if !slices.Equal(stale, []string{"f"}) {
		t.Errorf("eviction stale = %v, want the copies of ID 1", stale)
	}
	if o.has(1) {
		t.Error("ID 1 still owned after eviction")
	}
	if !o.has(0) || !o.has(maxOwnedIDs) {
		t.Error("ID 0 or the new ID missing")
	}
	if len(o.files) != maxOwnedIDs || len(o.order) != maxOwnedIDs {
		t.Errorf("size = %d/%d, want %d", len(o.files), len(o.order), maxOwnedIDs)
	}
	if got := o.remove(0); !slices.Equal(got, []string{"new0"}) {
		t.Errorf("remove(0) = %v", got)
	}
	if o.has(0) || len(o.order) != maxOwnedIDs-1 {
		t.Error("remove did not forget ID 0")
	}
}

func TestEarlySignals(t *testing.T) {
	now := time.Unix(100, 0)
	var e earlySignals
	sig := func(name string) *dbus.Signal { return &dbus.Signal{Name: name} }

	e.add(5, sig("a"), now)
	e.add(6, sig("b"), now)
	e.add(5, sig("c"), now)
	got := e.take(5, now)
	if len(got) != 2 || got[0].Name != "a" || got[1].Name != "c" {
		t.Fatalf("take(5) = %v, want a then c", got)
	}
	if got := e.take(5, now); len(got) != 0 {
		t.Errorf("second take(5) = %v, want empty", got)
	}

	if got := e.take(6, now.Add(earlySignalTTL+time.Millisecond)); len(got) != 0 {
		t.Errorf("take(6) after TTL = %v, want expired", got)
	}

	for i := range uint32(maxEarlySignals + 5) {
		e.add(i, sig("x"), now)
	}
	if len(e.buf) != maxEarlySignals {
		t.Errorf("buffer size = %d, want cap %d", len(e.buf), maxEarlySignals)
	}
	if got := e.take(0, now); len(got) != 0 {
		t.Error("oldest entry not dropped at the cap")
	}
}
