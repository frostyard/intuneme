package notify

import (
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

func TestFilterHints(t *testing.T) {
	got := filterHints(map[string]dbus.Variant{
		"urgency":                      dbus.MakeVariant(byte(1)),
		"suppress-sound":               dbus.MakeVariant(true),
		"category":                     dbus.MakeVariant("email.arrived"),
		"image-path":                   dbus.MakeVariant("/tmp/d/icon.png"),
		"desktop-entry":                dbus.MakeVariant("microsoft-edge"),
		"sound-file":                   dbus.MakeVariant("/home/u/.ssh/id_rsa"),
		"sound-name":                   dbus.MakeVariant("message-new-email"),
		"image-data":                   dbus.MakeVariant([]byte{1, 2, 3}),
		"x-kde-reply-placeholder-text": dbus.MakeVariant("Reply"),
		"transient":                    dbus.MakeVariant("yes"), // wrong type
	})
	want := []string{"urgency", "suppress-sound", "category", "image-path"}
	if len(got) != len(want) {
		t.Errorf("hints = %v, want only %v", got, want)
	}
	for _, k := range want {
		if _, ok := got[k]; !ok {
			t.Errorf("hint %s dropped, want kept", k)
		}
	}
}

func TestRateLimiter(t *testing.T) {
	now := time.Unix(0, 0)
	l := newRateLimiter(3, time.Second, func() time.Time { return now })

	for i := range 3 {
		if !l.allow() {
			t.Fatalf("call %d refused within the burst", i)
		}
	}
	if l.allow() {
		t.Fatal("call allowed after the burst")
	}
	now = now.Add(time.Second)
	if !l.allow() {
		t.Fatal("call refused after one refill period")
	}
	if l.allow() {
		t.Fatal("second call allowed after one refill period")
	}
	now = now.Add(time.Hour)
	for i := range 3 {
		if !l.allow() {
			t.Fatalf("call %d refused after a long wait", i)
		}
	}
	if l.allow() {
		t.Fatal("bucket filled past the burst")
	}
}
