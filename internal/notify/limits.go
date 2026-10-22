package notify

import (
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

// allowedHints are the Notify hints passed to the host server, with the D-Bus
// signature each must have. Every other hint is dropped: a container process
// chooses them all, and hints such as sound-file or desktop-entry name host
// files or apps. The image path hints are mapped to host copies afterwards.
var allowedHints = map[string]string{
	"urgency":        "y",
	"category":       "s",
	"transient":      "b",
	"resident":       "b",
	"suppress-sound": "b",
	"action-icons":   "b",
	"image-path":     "s",
	"image_path":     "s",
}

// filterHints returns the allowed hints that have the expected type.
func filterHints(hints map[string]dbus.Variant) map[string]dbus.Variant {
	out := make(map[string]dbus.Variant, len(hints))
	for k, v := range hints {
		if sig, ok := allowedHints[k]; ok && v.Signature().String() == sig {
			out[k] = v
		}
	}
	return out
}

// Notify rate limit: a burst of notifyBurst, then one per notifyRefill.
const (
	notifyBurst  = 10
	notifyRefill = time.Second
)

// rateLimiter is a token bucket.
type rateLimiter struct {
	mu     sync.Mutex
	tokens float64
	last   time.Time
	burst  float64
	refill time.Duration
	now    func() time.Time
}

func newRateLimiter(burst int, refill time.Duration, now func() time.Time) *rateLimiter {
	return &rateLimiter{tokens: float64(burst), last: now(), burst: float64(burst), refill: refill, now: now}
}

// allow takes a token and reports whether one was available.
func (l *rateLimiter) allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.tokens = min(l.burst, l.tokens+float64(now.Sub(l.last))/float64(l.refill))
	l.last = now
	if l.tokens < 1 {
		return false
	}
	l.tokens--
	return true
}
