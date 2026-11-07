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

// elastic gives a scrolling list rubber-band edges, as on macOS: scrolling
// past either end pulls the content a little further, against growing
// resistance, and it springs back once the scrolling stops.
type elastic struct {
	scroll gesture.Scroll
	offset float32   // pixels; positive past the start, negative past the end
	input  time.Time // when scrolling past an end last happened
	frame  time.Time // the previous frame
}

const (
	stretchLimit = 110 // dp; how far content can be pulled
	springRate   = 16  // per second; how fast it springs back
)

// layout lays out w, which lays out l. The list takes the scrolling it can
// use; this takes what is left over.
func (e *elastic) layout(gtx C, l *layout.List, w layout.Widget) D {
	limit := float32(gtx.Dp(stretchLimit))
	unbounded := pointer.ScrollRange{Min: math.MinInt32 / 2, Max: math.MaxInt32 / 2}
	if d := e.scroll.Update(gtx.Metric, gtx.Source, gtx.Now, gesture.Vertical, pointer.ScrollRange{}, unbounded); d != 0 {
		atStart := l.Position.First == 0 && l.Position.Offset <= 0
		atEnd := !l.Position.BeforeEnd
		switch {
		case d < 0 && !atStart, d > 0 && !atEnd:
			// The list's scroll range is from the previous frame, so the
			// first scroll after changing direction can end up here.
			l.Position.Offset += d
			l.Position.BeforeEnd = true
		default:
			resistance := 1 - min(abs32(e.offset)/limit, 1)
			e.offset = max(-limit, min(limit, e.offset-float32(d)*0.5*resistance))
			e.input = gtx.Now
		}
	}
	if e.offset != 0 && gtx.Now.Sub(e.input) > 90*time.Millisecond {
		dt := min(gtx.Now.Sub(e.frame).Seconds(), 0.05)
		e.offset *= float32(math.Exp(-dt * springRate))
		if abs32(e.offset) < 0.5 {
			e.offset = 0
		}
	}
	e.frame = gtx.Now
	if e.offset != 0 {
		gtx.Execute(op.InvalidateCmd{})
	}

	// The list's scroll area is inside this one, so that scrolling the
	// list cannot use goes to this.
	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
	e.scroll.Add(gtx.Ops)
	defer op.Offset(image.Pt(0, int(math.Round(float64(e.offset))))).Push(gtx.Ops).Pop()
	dims := w(gtx)
	return D{Size: gtx.Constraints.Max, Baseline: dims.Baseline}
}

func abs32(f float32) float32 {
	if f < 0 {
		return -f
	}
	return f
}
