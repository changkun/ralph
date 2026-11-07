package main

import (
	"image"
	"image/color"
	"math"
	"time"

	"gioui.org/gesture"
	"gioui.org/io/event"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/changkun/ralph/cmd/ralph-gui/internal/trackpad"
)

// splitterWidth is the width of the handle between two columns.
const splitterWidth unit.Dp = 7

// splitter is a handle between two columns that sets the width of the one
// before it.
type splitter struct {
	drag   gesture.Drag
	origin float32 // the handle's left edge in the window, in the last frame
	grab   float32 // where on the handle the pointer took hold
}

// layout lays out the handle for a column that starts at left, in pixels
// from the window's edge, and is *width wide, which dragging changes within
// [lo, hi]. It reports whether a drag ended, and fills the handle's halves
// with the colors of the columns on either side.
func (s *splitter) layout(gtx C, left int, width *unit.Dp, lo, hi unit.Dp, before, after color.NRGBA) (D, bool) {
	ended := false
	for {
		e, ok := s.drag.Update(gtx.Metric, gtx.Source, gesture.Horizontal)
		if !ok {
			break
		}
		switch e.Kind {
		case pointer.Press:
			s.grab = e.Position.X
		case pointer.Drag:
			// Events are relative to where the handle was last drawn.
			x := s.origin + e.Position.X - s.grab
			*width = clampDp(unit.Dp((x-float32(left))/gtx.Metric.PxPerDp), lo, hi)
		case pointer.Release, pointer.Cancel:
			ended = true
		}
	}
	*width = clampDp(*width, lo, hi)
	s.origin = float32(left + gtx.Dp(*width))

	size := image.Pt(gtx.Dp(splitterWidth), gtx.Constraints.Max.Y)
	half := size.X / 2
	fillRect(gtx, image.Rect(0, 0, half, size.Y), before)
	fillRect(gtx, image.Rect(half, 0, size.X, size.Y), after)
	rule := line
	if s.drag.Dragging() {
		rule = accent
	}
	fillRect(gtx, image.Rect(half, 0, half+max(1, gtx.Dp(1)), size.Y), rule)

	defer clip.Rect{Max: size}.Push(gtx.Ops).Pop()
	pointer.CursorColResize.Add(gtx.Ops)
	s.drag.Add(gtx.Ops)
	return D{Size: size}, ended
}

func clampDp(v, lo, hi unit.Dp) unit.Dp {
	if hi < lo {
		hi = lo
	}
	return max(lo, min(v, hi))
}

func fillRect(gtx C, r image.Rectangle, c color.NRGBA) {
	defer clip.Rect(r).Push(gtx.Ops).Pop()
	paint.ColorOp{Color: c}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

// scroller scrolls a list as macOS does. The content follows the fingers
// on the trackpad; past either end, it stretches with growing resistance
// and springs back once they lift. Momentum that carries the content to an
// end bounces off it. A mouse wheel stops at the ends. A thin scroll
// indicator shows only while scrolling.
type scroller struct {
	y, v     float32       // the stretch past an end in pixels, positive past the end, and its velocity
	frac     float32       // scrolling not yet applied to the list, less than a pixel
	speed    float32       // scrolling speed in pixels per second, by the events' times
	last     time.Duration // the time of the latest scroll event
	gesture  uint64        // the trackpad gesture that last scrolled the list
	touching bool          // whether that gesture's fingers are on the trackpad
	ignore   uint64        // a gesture whose momentum is ignored, because it bounced or the fingers let go of a stretch
	frame    time.Time     // the previous frame
	shown    time.Time     // when the scroll indicator was last needed

	// The height of each element, or -1 if not yet measured, and the
	// constraints and width the list lays them out with. The indicator's
	// place comes from these, rather than from the heights of the
	// elements in view, which vary too much for it to hold still.
	sizes      []int
	cs         layout.Constraints
	width      int
	measureAll bool // whether to measure elements out of view too, not only those it has shown
}

// scrollKind is what scrolls a list.
type scrollKind int

const (
	wheelScroll    scrollKind = iota // a mouse wheel, which stops at the ends
	fingerScroll                     // fingers on a trackpad, which stretch the content past the ends
	momentumScroll                   // the momentum after they lift, which bounces off the ends
)

// The trackpad's state, which tests replace.
var (
	scrollPhaseOf = trackpad.Lookup
	scrollGesture = trackpad.Current
)

const (
	rubberBand    = 0.55                   // how stiff stretching is; the constant Apple uses
	springFreq    = 16.5                   // per second; the stretch settles in about 0.35s
	maxBounce     = 3000                   // dp per second; the fastest a bounce starts
	indicatorHold = 700 * time.Millisecond // how long the indicator stays after scrolling
	indicatorFade = 250 * time.Millisecond
	measureBudget = 3 * time.Millisecond // how long a frame may spend measuring elements out of view
)

// layout lays out n elements of l, as material.List does.
func (s *scroller) layout(gtx C, th *material.Theme, l *widget.List, n int, el layout.ListElement) D {
	size := gtx.Constraints.Max
	s.update(gtx, &l.List, n, float32(size.Y))
	if size.X != s.width {
		s.width = size.X
		s.forget()
	}
	for len(s.sizes) < n {
		s.sizes = append(s.sizes, -1)
	}
	s.sizes = s.sizes[:n]

	shift := image.Pt(0, -int(math.Round(float64(s.y))))
	l.List.Layout(gtx, n, func(gtx C, i int) D {
		s.cs = gtx.Constraints
		defer op.Offset(shift).Push(gtx.Ops).Pop()
		dims := el(gtx, i)
		s.sizes[i] = dims.Size.Y
		return dims
	})
	s.measure(gtx, el)

	sb := material.Scrollbar(th, &l.Scrollbar)
	sb.Track.Color = color.NRGBA{}
	sb.Track.MinorPadding = 2
	sb.Indicator.MinorWidth = 7
	sb.Indicator.CornerRadius = 3.5
	if s.touching || l.Scrollbar.Dragging() || l.Scrollbar.IndicatorHovered() {
		s.shown = gtx.Now
	}
	alpha := 1 - min(1, max(0, float32(gtx.Now.Sub(s.shown)-indicatorHold)/float32(indicatorFade)))
	if alpha > 0 {
		gtx.Execute(op.InvalidateCmd{})
	}
	sb.Indicator.Color = color.NRGBA{A: uint8(0x60 * alpha)}
	sb.Indicator.HoverColor = color.NRGBA{A: uint8(0x90 * alpha)}
	hs := s.heights()
	top, total := offset(hs, l.Position), float32(0)
	for _, h := range hs {
		total += h
	}
	total = max(total, 1)
	gtx.Constraints.Min = size
	layout.E.Layout(gtx, func(gtx C) D {
		return sb.Layout(gtx, layout.Vertical, top/total, (top+float32(size.Y))/total)
	})
	if d := l.Scrollbar.ScrollDistance(); d != 0 {
		jump(&l.List, hs, top+d*total)
	}

	// Take all vertical scrolling over the list, on top of it, and let
	// other events through to the list and its content.
	defer pointer.PassOp{}.Push(gtx.Ops).Pop()
	defer clip.Rect{Max: size}.Push(gtx.Ops).Pop()
	event.Op(gtx.Ops, s)
	// Fill the whole area even when the content is shorter: a layout that
	// aligns its children, such as a Stack, would otherwise move the list
	// away from the top, and the area that takes scrolling with it.
	return D{Size: size}
}

// forget forgets the heights of the elements, which have changed.
func (s *scroller) forget() {
	for i := range s.sizes {
		s.sizes[i] = -1
	}
}

// measure measures elements out of view that are not yet measured, for a
// while each frame, if s measures all its elements.
func (s *scroller) measure(gtx C, el layout.ListElement) {
	if !s.measureAll || s.cs == (layout.Constraints{}) {
		return // or the list has laid nothing out to measure with
	}
	start := time.Now()
	gtx.Constraints = s.cs
	m := op.Record(gtx.Ops)
	defer m.Stop() // and drop what was drawn
	for i, h := range s.sizes {
		if h >= 0 {
			continue
		}
		if time.Since(start) > measureBudget {
			gtx.Execute(op.InvalidateCmd{}) // to measure the rest
			return
		}
		s.sizes[i] = el(gtx, i).Size.Y
	}
}

// heights returns the height of each element: as measured, or else the
// average of those measured.
func (s *scroller) heights() []float32 {
	sum, n := 0, 0
	for _, h := range s.sizes {
		if h >= 0 {
			sum, n = sum+h, n+1
		}
	}
	avg := float32(sum) / float32(max(n, 1))
	hs := make([]float32, len(s.sizes))
	for i, h := range s.sizes {
		hs[i] = avg
		if h >= 0 {
			hs[i] = float32(h)
		}
	}
	return hs
}

// offset returns how far down elements of heights hs p is.
func offset(hs []float32, p layout.Position) float32 {
	y := float32(p.Offset)
	for _, h := range hs[:min(p.First, len(hs))] {
		y += h
	}
	return y
}

// jump moves l to y pixels down its elements, of heights hs.
func jump(l *layout.List, hs []float32, y float32) {
	if y < offset(hs, l.Position) {
		l.Position.BeforeEnd = true
	}
	first := 0
	for first < len(hs)-1 && y >= hs[first] {
		y -= hs[first]
		first++
	}
	l.Position.First, l.Position.Offset = first, int(max(0, y))
	l.Position.Count = 0 // unknown until the list is laid out again
}

// update applies this frame's scrolling to l, which has n elements and a
// viewport height pixels tall, and moves the stretch on.
func (s *scroller) update(gtx C, l *layout.List, n int, height float32) {
	dt := float32(min(gtx.Now.Sub(s.frame).Seconds(), 0.05))
	if s.frame.IsZero() {
		dt = 0
	}
	s.frame = gtx.Now
	unbounded := pointer.ScrollRange{Min: math.MinInt32 / 2, Max: math.MaxInt32 / 2}
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: s, Kinds: pointer.Scroll, ScrollY: unbounded})
		if !ok {
			break
		}
		if e, ok := ev.(pointer.Event); ok {
			s.handle(gtx, l, n, e, height)
		}
	}

	// Gio drops the event that ends a gesture, which scrolls nothing, so
	// ask the trackpad whether the fingers are still down.
	down, g := scrollGesture()
	touching := down && g == s.gesture && g != 0
	if s.touching && !touching && s.y != 0 {
		s.ignore = s.gesture // the stretch springs back instead
	}
	s.touching = touching
	if touching {
		gtx.Execute(op.InvalidateCmd{}) // to see them lift
		return                          // the stretch stays where they put it
	}
	if s.y == 0 && s.v == 0 {
		return
	}
	gtx.Execute(op.InvalidateCmd{})
	// A critically damped spring pulls the stretch back to 0.
	for t := dt; t > 0; t -= 1.0 / 240 {
		h := min(t, 1.0/240)
		s.v += (-springFreq*springFreq*s.y - 2*springFreq*s.v) * h
		s.y += s.v * h
	}
	if abs32(s.y) < 0.5 && abs32(s.v) < 20 {
		s.y, s.v = 0, 0
	}
}

// handle applies a scroll event.
func (s *scroller) handle(gtx C, l *layout.List, n int, e pointer.Event, height float32) {
	d := e.Scroll.Y
	if dt := (e.Time - s.last).Seconds(); dt > 0 && dt < 0.1 {
		s.speed = 0.5*s.speed + 0.5*d/float32(dt)
	} else {
		s.speed = 0
	}
	s.last = e.Time
	if d == 0 {
		return
	}
	s.shown = gtx.Now
	ph, ok := scrollPhaseOf(e.Time)
	switch {
	case !ok || ph.Finger == 0 && ph.Momentum == 0:
		s.scrollBy(gtx, l, n, d, height, wheelScroll)
	case ph.Finger != 0:
		if ph.Gesture != s.gesture {
			// Fingers down again catch the content where it is.
			s.gesture, s.v = ph.Gesture, 0
		}
		s.touching = true
		s.scrollBy(gtx, l, n, d, height, fingerScroll)
	case ph.Gesture == s.ignore:
	case s.y != 0:
		// The fingers let go of a stretch, which springs back.
		s.ignore = ph.Gesture
	default:
		s.gesture = ph.Gesture
		s.scrollBy(gtx, l, n, d, height, momentumScroll)
	}
}

// scrollBy scrolls d pixels: first out of any stretch, then through the
// list, then past its end.
func (s *scroller) scrollBy(gtx C, l *layout.List, n int, d, height float32, kind scrollKind) {
	if kind == fingerScroll && s.y != 0 && (d > 0) != (s.y > 0) {
		x := unstretch(s.y, height) + d
		if (x > 0) != (s.y > 0) {
			s.y, d = 0, x
		} else {
			s.y, d = stretch(x, height), 0
		}
	}
	if d != 0 && s.y == 0 {
		// How far the list can move, as the list itself works it out
		// from its last layout and the scrolling since.
		lo, hi := float32(math.Inf(-1)), float32(math.Inf(1))
		if l.Position.First == 0 {
			lo = min(0, -float32(l.Position.Offset))
		}
		if l.Position.First+l.Position.Count >= n {
			hi = max(0, -float32(l.Position.OffsetLast))
		}
		moved := max(lo, min(d, hi))
		s.frac += moved
		step := int(s.frac)
		s.frac -= float32(step)
		l.Position.Offset += step
		l.Position.OffsetLast += step
		if moved < 0 {
			l.Position.BeforeEnd = true
		}
		d -= moved
	}
	if d == 0 {
		return
	}
	switch kind {
	case fingerScroll:
		s.y = stretch(unstretch(s.y, height)+d, height)
	case momentumScroll:
		// Reaching an end bounces off it, and ignores the rest.
		limit := float32(gtx.Dp(maxBounce))
		s.v = min(abs32(s.speed), limit)
		if d < 0 {
			s.v = -s.v
		}
		s.ignore = s.gesture
	}
}

// stretch returns how far content moves when scrolled x pixels past an end
// of a viewport height pixels tall: less and less, never as far as height.
func stretch(x, height float32) float32 {
	y := (1 - 1/(abs32(x)*rubberBand/height+1)) * height
	if x < 0 {
		return -y
	}
	return y
}

// unstretch is the inverse of stretch.
func unstretch(y, height float32) float32 {
	a := min(abs32(y), 0.99*height)
	x := (1/(1-a/height) - 1) * height / rubberBand
	if y < 0 {
		return -x
	}
	return x
}

func abs32(f float32) float32 {
	if f < 0 {
		return -f
	}
	return f
}
