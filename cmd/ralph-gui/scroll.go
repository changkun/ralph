package main

import (
	"image"
	"image/color"
	"math"
	"time"

	"gioui.org/gesture"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
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

// scroller scrolls a list as macOS does. The list follows the trackpad or
// wheel; past either end, the content stretches with growing resistance and
// springs back when the scrolling stops, and reaching an end at speed makes
// it bounce. A thin scroll indicator shows only while scrolling.
//
// Gio reports scroll deltas but not whether a finger is on the trackpad, so
// a pause in scrolling stands for the finger lifting.
type scroller struct {
	scroll   gesture.Scroll
	y, v     float32   // the stretch past an end in pixels, positive past the end, and its velocity
	speed    float32   // scrolling speed in pixels per second
	bouncing bool      // whether the stretch is a bounce, which ignores further scrolling the same way
	input    time.Time // when scrolling last happened
	frame    time.Time // the previous frame
	shown    time.Time // when the scroll indicator was last needed
}

const (
	rubberBand    = 0.55                   // how stiff stretching is; the constant Apple uses
	releaseAfter  = 80 * time.Millisecond  // a pause this long ends a scroll
	springFreq    = 16.5                   // per second; the stretch settles in about 0.35s
	bounceSpeed   = 1200                   // dp per second; reaching an end faster bounces
	maxBounce     = 3000                   // dp per second; the fastest a bounce starts
	indicatorHold = 700 * time.Millisecond // how long the indicator stays after scrolling
	indicatorFade = 250 * time.Millisecond
)

// layout lays out n elements of l, as material.List does.
func (s *scroller) layout(gtx C, th *material.Theme, l *widget.List, n int, el layout.ListElement) D {
	size := gtx.Constraints.Max
	s.update(gtx, &l.List, n, float32(size.Y))

	ls := material.List(th, l)
	ls.AnchorStrategy = material.Overlay
	ls.Track.Color = color.NRGBA{}
	ls.Track.MinorPadding = 2
	ls.Indicator.MinorWidth = 7
	ls.Indicator.CornerRadius = 3.5
	if l.Scrollbar.Dragging() || l.Scrollbar.IndicatorHovered() {
		s.shown = gtx.Now
	}
	alpha := 1 - min(1, max(0, float32(gtx.Now.Sub(s.shown)-indicatorHold)/float32(indicatorFade)))
	if alpha > 0 {
		gtx.Execute(op.InvalidateCmd{})
	}
	ls.Indicator.Color = color.NRGBA{A: uint8(0x60 * alpha)}
	ls.Indicator.HoverColor = color.NRGBA{A: uint8(0x90 * alpha)}

	shift := image.Pt(0, -int(math.Round(float64(s.y))))
	ls.Layout(gtx, n, func(gtx C, i int) D {
		defer op.Offset(shift).Push(gtx.Ops).Pop()
		return el(gtx, i)
	})

	// Take all scrolling over the list, on top of it, and let other
	// events through to the list and its content.
	defer pointer.PassOp{}.Push(gtx.Ops).Pop()
	defer clip.Rect{Max: size}.Push(gtx.Ops).Pop()
	s.scroll.Add(gtx.Ops)
	// Fill the whole area even when the content is shorter: a layout that
	// aligns its children, such as a Stack, would otherwise move the list
	// away from the top, and the area that takes scrolling with it.
	return D{Size: size}
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
	released := gtx.Now.Sub(s.input) > releaseAfter
	d := float32(s.scroll.Update(gtx.Metric, gtx.Source, gtx.Now, gesture.Vertical, pointer.ScrollRange{}, unbounded))
	if d != 0 {
		if released { // a new scroll
			s.speed, s.bouncing = 0, false
		}
		if dt > 0 {
			s.speed = 0.5*s.speed + 0.5*d/dt
		}
		s.input, s.shown, released = gtx.Now, gtx.Now, false
		s.scrollBy(gtx, l, n, d, height)
	}
	if s.y == 0 && s.v == 0 {
		return
	}
	gtx.Execute(op.InvalidateCmd{})
	if !released && !s.bouncing {
		return // held where the scrolling put it
	}
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

// scrollBy scrolls d pixels: first out of any stretch, then through the
// list, then past its end.
func (s *scroller) scrollBy(gtx C, l *layout.List, n int, d, height float32) {
	if s.bouncing {
		if (d > 0) == (s.speed > 0) {
			return // the rest of the scroll that bounced
		}
		s.bouncing = false
	}
	if s.y != 0 && (d > 0) != (s.y > 0) {
		x := unstretch(s.y, height) + d
		if (x > 0) != (s.y > 0) {
			s.y, d = 0, x
		} else {
			s.y, d = stretch(x, height), 0
		}
		s.v = 0
	}
	if d != 0 && s.y == 0 {
		// How far the list can move, as of its last layout, as the list
		// itself works it out.
		lo, hi := float32(math.Inf(-1)), float32(math.Inf(1))
		if l.Position.First == 0 {
			lo = min(0, -float32(l.Position.Offset))
		}
		if l.Position.First+l.Position.Count >= n {
			hi = max(0, -float32(l.Position.OffsetLast))
		}
		moved := max(lo, min(d, hi))
		if moved != 0 {
			l.Position.Offset += int(moved)
			if moved < 0 {
				l.Position.BeforeEnd = true
			}
		}
		d -= moved
	}
	if d == 0 {
		return
	}
	if abs32(s.speed) > float32(gtx.Dp(bounceSpeed)) {
		// Reaching an end at speed bounces off it.
		limit := float32(gtx.Dp(maxBounce))
		s.bouncing, s.v = true, max(-limit, min(s.speed, limit))
		return
	}
	s.y, s.v = stretch(unstretch(s.y, height)+d, height), 0
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
