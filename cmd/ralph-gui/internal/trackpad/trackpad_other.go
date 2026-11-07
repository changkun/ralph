//go:build !darwin

package trackpad

import "time"

func lookup(time.Duration) (Event, bool) { return Event{}, false }
func current() (bool, uint64)            { return false, 0 }
func watch()                             {}
