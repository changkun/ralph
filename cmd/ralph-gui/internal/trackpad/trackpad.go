// Package trackpad tells where scroll events are in a trackpad gesture:
// whether fingers are moving on the trackpad, or the content is coasting
// after they lifted. Gio reports only how far each event scrolls, so on
// macOS this package watches the app's events for the phases AppKit
// gives them. Elsewhere it knows nothing.
package trackpad

import "time"

// Phase is a set of phases, with the values of AppKit's NSEventPhase.
type Phase uint8

const (
	Began      Phase = 1 << 0
	Stationary Phase = 1 << 1
	Changed    Phase = 1 << 2
	Ended      Phase = 1 << 3
	Cancelled  Phase = 1 << 4
	MayBegin   Phase = 1 << 5
)

// Event is where a scroll event is in a gesture. A mouse wheel's events
// have neither phase.
type Event struct {
	Finger   Phase  // while fingers move the content
	Momentum Phase  // while the content coasts after they lift
	Gesture  uint64 // numbers the gestures, from 1, a new one each time fingers touch the trackpad
}

// Lookup returns where the scroll event at time t, as Gio's
// pointer.Event.Time gives it, is in its gesture. It reports false if it
// does not know.
func Lookup(t time.Duration) (Event, bool) { return lookup(t) }

// Current reports whether fingers are on the trackpad, and the number of
// the latest gesture.
func Current() (down bool, gesture uint64) { return current() }

// Watch starts watching the app's scroll events. It must be called before
// app.Main.
func Watch() { watch() }
