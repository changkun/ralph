//go:build darwin

package trackpad

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit

#import <AppKit/AppKit.h>
#include <math.h>
#include <pthread.h>

// The phases of the latest scroll events, by the events' timestamps, and
// the state of the latest gesture.
typedef struct {
	double time;
	int phase, momentum;
	unsigned long gesture;
} scrollPhase;

enum { maxPhases = 256 };
static scrollPhase phases[maxPhases];
static unsigned long nphases, gesture;
static int touching;
static double touched; // the uptime of the latest event with fingers on the trackpad
static pthread_mutex_t mu = PTHREAD_MUTEX_INITIALIZER;

static double uptime(void) {
	return [[NSProcessInfo processInfo] systemUptime];
}

// fingersDown reports whether fingers are on the trackpad. A gesture whose
// end the app missed, say because another app took the focus, counts as
// over after a while.
static int fingersDown(void) {
	return touching && uptime() - touched < 2;
}

static void watch(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		[NSEvent addLocalMonitorForEventsMatchingMask:NSEventMaskScrollWheel handler:^NSEvent *(NSEvent *e) {
			int phase = (int)e.phase;
			pthread_mutex_lock(&mu);
			if (phase & (NSEventPhaseMayBegin | NSEventPhaseBegan)) {
				if (!fingersDown()) {
					gesture++;
				}
				touching = 1;
			}
			if (phase != NSEventPhaseNone) {
				touched = uptime();
			}
			if (phase & (NSEventPhaseEnded | NSEventPhaseCancelled)) {
				touching = 0;
			}
			phases[nphases++ % maxPhases] = (scrollPhase){e.timestamp, phase, (int)e.momentumPhase, gesture};
			pthread_mutex_unlock(&mu);
			return e;
		}];
	});
}

static int lookup(double t, scrollPhase *out) {
	int found = 0;
	pthread_mutex_lock(&mu);
	for (unsigned long i = 0; i < maxPhases && i < nphases; i++) {
		scrollPhase p = phases[(nphases - 1 - i) % maxPhases];
		if (fabs(p.time - t) < 1e-6) {
			*out = p;
			found = 1;
			break;
		}
	}
	pthread_mutex_unlock(&mu);
	return found;
}

static void current(int *down, unsigned long *g) {
	pthread_mutex_lock(&mu);
	*down = fingersDown();
	*g = gesture;
	pthread_mutex_unlock(&mu);
}
*/
import "C"

import "time"

func lookup(t time.Duration) (Event, bool) {
	var p C.scrollPhase
	// Gio sets an event's time to AppKit's timestamp, in nanoseconds.
	if C.lookup(C.double(t.Seconds()), &p) == 0 {
		return Event{}, false
	}
	return Event{Finger: Phase(p.phase), Momentum: Phase(p.momentum), Gesture: uint64(p.gesture)}, true
}

func current() (bool, uint64) {
	var down C.int
	var g C.ulong
	C.current(&down, &g)
	return down != 0, uint64(g)
}

func watch() { C.watch() }
