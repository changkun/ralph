package main

import (
	"fmt"
	"image"
	"image/color"
	"strconv"
	"strings"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"gioui.org/x/styledtext"

	"github.com/changkun/ralph/internal/backend"
	"github.com/changkun/ralph/internal/loop"
	"github.com/changkun/ralph/internal/session"
	"github.com/changkun/ralph/internal/transcript"
)

type (
	C = layout.Context
	D = layout.Dimensions
)

func rgb(c uint32) color.NRGBA {
	return color.NRGBA{R: uint8(c >> 16), G: uint8(c >> 8), B: uint8(c), A: 0xff}
}

// Colors, after Slack's.
var (
	white     = rgb(0xffffff)
	ink       = rgb(0x1d1c1d)
	muted     = rgb(0x616061)
	faint     = rgb(0x8a898a)
	line      = rgb(0xdddddd)
	panel     = rgb(0xf8f8f8)
	selected  = rgb(0xe3eefa)
	accent    = rgb(0x1264a3)
	go_       = rgb(0x007a5a)
	danger    = rgb(0xd0164f)
	codeBg    = rgb(0xf6f6f6)
	codeLine  = rgb(0xe2e2e2)
	codeInk   = rgb(0xc01343)
	quoteBar  = rgb(0xdddddd)
	changeInk = rgb(0xa0522d)
)

var roleColors = map[backend.Role]color.NRGBA{
	backend.Standalone: rgb(0x2b6cb0),
	backend.Strategist: rgb(0x6d3fc0),
	backend.Executor:   rgb(0x1264a3),
	backend.Evaluator:  rgb(0x007a5a),
	backend.Archivist:  rgb(0xa0522d),
}

const (
	lineScale      = 1.42 // line height, as a multiple of the text size
	maxOutputLines = 12   // tool output is cut to this many lines until expanded
	avatarSize     = 32   // dp
	gutter         = 44   // dp; where a turn's events start, right of its avatar
	minPaneWidth   = 360  // dp; the narrowest the transcript pane may get
)

func (u *UI) layout(gtx C) D {
	u.update(gtx)
	paint.Fill(gtx.Ops, white)
	winDp := unit.Dp(float32(gtx.Constraints.Max.X) / gtx.Metric.PxPerDp)
	dims := layout.Flex{}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return column(gtx, u.prefs.SidebarWidth, u.layoutSidebar) }),
		layout.Rigid(func(gtx C) D {
			hi := min(720, winDp-u.prefs.TurnsWidth-2*splitterWidth-minPaneWidth)
			d, ended := u.splitters[0].layout(gtx, 0, &u.prefs.SidebarWidth, 300, hi, panel, panel)
			if ended {
				u.prefs.save()
			}
			return d
		}),
		layout.Rigid(func(gtx C) D { return column(gtx, u.prefs.TurnsWidth, u.layoutTurns) }),
		layout.Rigid(func(gtx C) D {
			left := gtx.Dp(u.prefs.SidebarWidth) + gtx.Dp(splitterWidth)
			hi := min(440, winDp-u.prefs.SidebarWidth-2*splitterWidth-minPaneWidth)
			d, ended := u.splitters[1].layout(gtx, left, &u.prefs.TurnsWidth, 170, hi, panel, white)
			if ended {
				u.prefs.save()
			}
			return d
		}),
		layout.Flexed(1, u.layoutPane),
	)
	u.sync(gtx)
	return dims
}

// column lays out w in a fixed-width, full-height column.
func column(gtx C, width unit.Dp, w layout.Widget) D {
	px := gtx.Dp(width)
	gtx.Constraints = layout.Exact(image.Pt(px, gtx.Constraints.Max.Y))
	paint.FillShape(gtx.Ops, panel, clip.Rect{Max: gtx.Constraints.Max}.Op())
	w(gtx)
	return D{Size: gtx.Constraints.Max}
}

func hrule(gtx C) D {
	size := image.Pt(gtx.Constraints.Max.X, max(1, gtx.Dp(1)))
	fillRect(gtx, image.Rectangle{Max: size}, line)
	return D{Size: size}
}

func space(dp unit.Dp) layout.Widget {
	return func(gtx C) D { return layout.Spacer{Height: dp, Width: dp}.Layout(gtx) }
}

// leftBar lays out w with a colored bar down its left side.
func leftBar(gtx C, c color.NRGBA, w layout.Widget) D {
	m := op.Record(gtx.Ops)
	dims := layout.Inset{Left: 13}.Layout(gtx, w)
	call := m.Stop()
	fillRect(gtx, image.Rect(0, 0, gtx.Dp(3), dims.Size.Y), c)
	call.Add(gtx.Ops)
	return dims
}

// boxed lays out w padded, on bg, inside a rounded border.
func boxed(gtx C, bg, border color.NRGBA, pad unit.Dp, w layout.Widget) D {
	return widget.Border{Color: border, CornerRadius: 6, Width: 1}.Layout(gtx, func(gtx C) D {
		return layout.Background{}.Layout(gtx,
			func(gtx C) D {
				r := gtx.Dp(6)
				defer clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Min}, r).Push(gtx.Ops).Pop()
				paint.Fill(gtx.Ops, bg)
				return D{Size: gtx.Constraints.Min}
			},
			func(gtx C) D { return layout.UniformInset(pad).Layout(gtx, w) })
	})
}

// text returns a label in Lato.
func (u *UI) text(size unit.Sp, c color.NRGBA, txt string) material.LabelStyle {
	l := material.Label(u.th, size, txt)
	l.Color = c
	l.LineHeightScale = lineScale
	return l
}

func (u *UI) bold(size unit.Sp, c color.NRGBA, txt string) material.LabelStyle {
	l := u.text(size, c, txt)
	l.Font.Weight = font.Black
	return l
}

func (u *UI) code(size unit.Sp, c color.NRGBA, txt string) material.LabelStyle {
	l := u.text(size, c, txt)
	l.Font.Typeface = mono
	l.LineHeightScale = 1.3
	return l
}

// symbol returns a label for a symbol such as ✓, which Lato lacks.
func (u *UI) symbol(size unit.Sp, c color.NRGBA, txt string) material.LabelStyle {
	l := u.text(size, c, txt)
	l.Font.Typeface = mono
	return l
}

func (u *UI) heading(txt string) layout.Widget {
	return func(gtx C) D {
		return layout.Inset{Top: 22, Bottom: 8}.Layout(gtx, u.bold(13, ink, txt).Layout)
	}
}

// editor draws an editor in an outlined box.
func (u *UI) editor(gtx C, ed *widget.Editor, hint string, code bool) D {
	return boxed(gtx, white, line, 8, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		e := material.Editor(u.th, ed, hint)
		e.HintColor = faint
		e.LineHeightScale = 1.3
		if code {
			e.Font.Typeface = mono
			e.TextSize = 13
		}
		return e.Layout(gtx)
	})
}

func (u *UI) button(gtx C, btn *widget.Clickable, txt string, bg color.NRGBA) D {
	b := material.Button(u.th, btn, txt)
	b.Background = bg
	b.Font.Weight = font.Bold
	b.CornerRadius = 6
	b.Inset = layout.Inset{Top: 9, Bottom: 9, Left: 18, Right: 18}
	return b.Layout(gtx)
}

// enabled lays out w, disabled if the settings cannot change now.
func (u *UI) enabled(w layout.Widget) layout.Widget {
	return func(gtx C) D {
		if u.active() {
			gtx = gtx.Disabled()
		}
		return w(gtx)
	}
}

func (u *UI) layoutSidebar(gtx C) D {
	items := []layout.Widget{
		u.bold(24, ink, "Ralph").Layout,
		u.text(14, muted, "Runs coding agents in rounds on a project, committing it after each round.").Layout,
		u.heading("Project folder"),
		// Another folder can be opened while a session runs; the session
		// keeps running, and reopening its folder follows it again.
		func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx C) D { return u.editor(gtx, &u.folderEd, "Path, or choose a folder", false) }),
				layout.Rigid(space(8)),
				layout.Rigid(func(gtx C) D { return u.button(gtx, &u.chooseBtn, "Choose…", accent) }),
			)
		},
		u.layoutControls,
		u.heading("Where agents run"),
		u.enabled(func(gtx C) D {
			var children []layout.FlexChild
			for _, rt := range session.Runtimes {
				label := rt
				if rt == session.Host {
					label = "this machine"
				}
				children = append(children, layout.Rigid(material.RadioButton(u.th, &u.runtime, rt, label).Layout))
			}
			return layout.Flex{}.Layout(gtx, children...)
		}),
		u.layoutLogins,
		u.heading("Pattern"),
		u.enabled(func(gtx C) D {
			var children []layout.FlexChild
			for _, p := range session.Patterns {
				children = append(children, layout.Rigid(material.RadioButton(u.th, &u.pattern, string(p), string(p)).Layout))
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		}),
	}
	for _, role := range loop.Pattern(u.pattern.Value).Roles() {
		items = append(items, u.heading(roleTitle(role)), u.enabled(func(gtx C) D { return u.layoutRole(gtx, role) }))
	}
	items = append(items, space(40))
	return u.sidebarScroll.layout(gtx, u.th, &u.sidebar, len(items), func(gtx C, i int) D {
		top := unit.Dp(0)
		if i == 0 {
			top = 22
		}
		return layout.Inset{Left: 22, Right: 22, Top: top}.Layout(gtx, items[i])
	})
}

// layoutControls lays out the Start and Stop buttons, how many rounds a
// start runs, the session's state, and any problem, with a button to make
// the folder a git repository if it is not one.
func (u *UI) layoutControls(gtx C) D {
	active := u.active()
	status, bad := u.statusText()
	statusColor := ink
	if bad {
		statusColor = danger
	}
	start, count, noun := "Start", "for", "rounds"
	if u.thinking() {
		count, noun = "with", "thinkers"
	} else if u.finished > 0 {
		start = "Continue"
	}
	children := []layout.FlexChild{
		layout.Rigid(space(18)),
		layout.Rigid(func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					if active || u.folder == "" || !u.isRepo && !u.thinking() {
						gtx = gtx.Disabled()
					}
					return u.button(gtx, &u.startBtn, start, go_)
				}),
				layout.Rigid(space(10)),
				layout.Rigid(u.text(15, ink, count).Layout),
				layout.Rigid(space(8)),
				layout.Rigid(u.enabled(func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Dp(64)
					gtx.Constraints.Max.X = gtx.Dp(64)
					if u.thinking() {
						return u.editor(gtx, &u.thinkers, "", false)
					}
					return u.editor(gtx, &u.rounds, "all", false)
				})),
				layout.Rigid(space(8)),
				layout.Rigid(u.text(15, ink, noun).Layout),
				layout.Flexed(1, space(8)),
				layout.Rigid(func(gtx C) D {
					if !active {
						gtx = gtx.Disabled()
					}
					return u.button(gtx, &u.stopBtn, "Stop", danger)
				}),
			)
		}),
		layout.Rigid(space(10)),
	}
	if !active {
		children = append(children, layout.Rigid(u.text(14, muted, u.roundsText()).Layout), layout.Rigid(space(4)))
	}
	children = append(children, layout.Rigid(u.text(15, statusColor, status).Layout))
	if u.folder != "" && !u.isRepo && !u.thinking() {
		children = append(children, layout.Rigid(space(10)),
			layout.Rigid(func(gtx C) D {
				gtx.Constraints.Min.X = 0
				return u.button(gtx, &u.initBtn, "Run git init", accent)
			}))
	}
	for _, problem := range []string{u.message, errText(u.invalid)} {
		if problem != "" {
			children = append(children, layout.Rigid(space(6)), layout.Rigid(u.text(14, danger, problem).Layout))
		}
	}
	if active {
		children = append(children, layout.Rigid(space(4)),
			layout.Rigid(u.text(14, muted, "Settings can be changed once the session stops.").Layout))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// roundsText says which rounds, or which thinkers, a start runs.
func (u *UI) roundsText() string {
	if u.thinking() {
		n, _ := strconv.Atoi(u.thinkers.Text())
		switch {
		case n < 1 || n > session.MaxThinkers:
			return ""
		case n == 1:
			return fmt.Sprintf("Runs thinker %d, alone in a new empty folder, %s, and a container of its own.",
				u.finished+1, session.ThinkerFolder(u.finished+1))
		}
		return fmt.Sprintf("Runs thinkers %d to %d side by side, each alone in a new empty folder, %s to %s, and a container of its own.",
			u.finished+1, u.finished+n, session.ThinkerFolder(u.finished+1), session.ThinkerFolder(u.finished+n))
	}
	n, _ := strconv.Atoi(u.rounds.Text())
	switch {
	case n == 0:
		return "Runs until you stop it."
	case n == 1:
		return fmt.Sprintf("Runs round %d, then stops.", u.finished+1)
	}
	return fmt.Sprintf("Runs rounds %d to %d, then stops.", u.finished+1, u.finished+n)
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (u *UI) layoutLogins(gtx C) D {
	if u.runtime.Value == session.Host {
		return layout.Inset{Top: 6}.Layout(gtx,
			u.text(14, muted, "Agents run directly on this machine, with the CLIs' own logins and permissions.").Layout)
	}
	where := "Agents run in a new container that sees only the project folder."
	if u.thinking() {
		where = "Each thinker runs in a new container that sees only its own empty folder, read-only."
	}
	children := []layout.FlexChild{layout.Rigid(u.text(14, muted, where).Layout)}
	for _, l := range u.logins {
		dot, txt := go_, fmt.Sprintf("%s logs in with %s.", backendTitle(l.Backend), strings.Join(l.Found, ", "))
		if len(l.Found) == 0 {
			dot, txt = danger, fmt.Sprintf("%s has no login: %s.", backendTitle(l.Backend), l.Help)
		}
		children = append(children, layout.Rigid(space(6)), layout.Rigid(func(gtx C) D {
			return layout.Flex{}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					return layout.Inset{Top: 1, Right: 8}.Layout(gtx, u.symbol(12, dot, "●").Layout)
				}),
				layout.Flexed(1, u.text(14, ink, txt).Layout),
			)
		}))
	}
	return layout.Inset{Top: 6}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

func (u *UI) layoutRole(gtx C, role backend.Role) D {
	f := u.roles[role]
	children := []layout.FlexChild{
		layout.Rigid(u.text(14, muted, roleInfo[role]).Layout),
		layout.Rigid(space(6)),
		layout.Rigid(func(gtx C) D {
			var row []layout.FlexChild
			for _, name := range backend.Names {
				row = append(row, layout.Rigid(material.RadioButton(u.th, &f.backend, name, name).Layout))
			}
			row = append(row, layout.Rigid(space(8)), layout.Flexed(1, func(gtx C) D {
				return u.editor(gtx, &f.model, "model (CLI default)", false)
			}))
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx, row...)
		}),
		layout.Rigid(material.CheckBox(u.th, &f.edit, "Edit prompts").Layout),
	}
	if f.edit.Value {
		children = append(children,
			layout.Rigid(u.text(14, muted, "System prompt").Layout),
			layout.Rigid(space(4)),
			layout.Rigid(func(gtx C) D { return u.editor(gtx, &f.system, "none", true) }),
			layout.Rigid(space(10)),
			layout.Rigid(u.text(14, muted, "Prompt").Layout),
			layout.Rigid(space(4)),
			layout.Rigid(func(gtx C) D { return u.editor(gtx, &f.prompt, "", true) }),
			layout.Rigid(space(10)),
			layout.Rigid(func(gtx C) D {
				b := material.Button(u.th, &f.reset, "Reset to the built-in prompts")
				b.Background, b.Color = white, accent
				b.TextSize, b.CornerRadius = 14, 6
				return widget.Border{Color: line, CornerRadius: 6, Width: 1}.Layout(gtx, b.Layout)
			}),
		)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

func (u *UI) layoutTurns(gtx C) D {
	if u.folder == "" {
		return D{Size: gtx.Constraints.Max}
	}
	n := len(u.turns) + firstTurnView + 1 // and space at the end
	return u.turnsScroll.layout(gtx, u.th, &u.turnList, n, func(gtx C, i int) D {
		switch {
		case i == viewLive:
			return layout.Inset{Top: 14}.Layout(gtx, func(gtx C) D {
				return u.layoutTurnItem(gtx, i, "Live", "", ink, accent)
			})
		case i == viewConsole:
			return u.layoutTurnItem(gtx, i, "Console", "", ink, faint)
		case i == n-1:
			return layout.Spacer{Height: 40}.Layout(gtx)
		}
		t := u.turns[i-firstTurnView]
		mark, ok, running := u.turnState(t)
		markColor := go_
		switch {
		case running:
			markColor = accent
		case !ok:
			markColor = danger
		}
		item := func(gtx C) D { return u.layoutTurnItem(gtx, i, turnTitle(t), mark, markColor, roleColors[t.Role]) }
		// Turns are grouped by round, and thinkers all together.
		group := fmt.Sprintf("Round %d", t.Round)
		if t.Thinker {
			group = "Thinkers"
		}
		if i > firstTurnView && (t.Thinker || u.turns[i-firstTurnView-1].Round == t.Round) {
			return item(gtx)
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				return layout.Inset{Left: 18, Top: 16, Bottom: 4}.Layout(gtx, u.bold(13, muted, group).Layout)
			}),
			layout.Rigid(item),
		)
	})
}

func (u *UI) layoutTurnItem(gtx C, i int, title, mark string, markColor, dot color.NRGBA) D {
	return layout.Inset{Left: 8, Right: 8}.Layout(gtx, func(gtx C) D {
		return material.Clickable(gtx, &u.turnBtns[i], func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			bg := color.NRGBA{}
			if i == u.view {
				bg = selected
			}
			return layout.Background{}.Layout(gtx,
				func(gtx C) D {
					defer clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Min}, gtx.Dp(6)).Push(gtx.Ops).Pop()
					paint.Fill(gtx.Ops, bg)
					return D{Size: gtx.Constraints.Min}
				},
				func(gtx C) D {
					return layout.Inset{Left: 10, Right: 10, Top: 5, Bottom: 5}.Layout(gtx, func(gtx C) D {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(func(gtx C) D {
								return layout.Inset{Right: 8}.Layout(gtx, u.symbol(10, dot, "●").Layout)
							}),
							layout.Flexed(1, u.text(15, ink, title).Layout),
							layout.Rigid(u.symbol(13, markColor, mark).Layout),
						)
					})
				})
		})
	})
}

func (u *UI) layoutPane(gtx C) D {
	title, subtitle := "Live", fmt.Sprintf("The latest %d rounds, as they happen", liveRounds)
	if u.thinking() {
		subtitle = "Every thinker, and what it concluded"
	}
	switch {
	case u.view == viewConsole:
		title, subtitle = "Console", "What the session printed"
	case u.view >= firstTurnView && u.view-firstTurnView < len(u.turns):
		t := u.turns[u.view-firstTurnView]
		title, subtitle = turnTitle(t), u.turnSubtitle(t)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 28, Right: 24, Top: 16, Bottom: 14}.Layout(gtx, func(gtx C) D {
				return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx C) D {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(u.bold(18, ink, title).Layout),
							layout.Rigid(u.text(13, muted, subtitle).Layout),
						)
					}),
					layout.Rigid(func(gtx C) D {
						if u.view == viewConsole {
							return D{}
						}
						return material.CheckBox(u.th, &u.markdownOn, "Format Markdown").Layout(gtx)
					}),
				)
			})
		}),
		layout.Rigid(hrule),
		layout.Flexed(1, u.layoutEntries),
	)
}

func (u *UI) layoutEntries(gtx C) D {
	switch {
	case u.folder == "":
		return u.placeholder(gtx, "Choose a project folder, which must be a git repository. Ralph commits it after each round, and keeps its logs in .ralph.")
	case len(u.entries) == 0 && u.view == viewConsole:
		return u.placeholder(gtx, "No session has run in this folder yet.")
	case len(u.entries) == 0:
		return u.placeholder(gtx, "Nothing yet. Press Start to run a session.")
	}
	// The live view and the console stay at their end, until the user
	// scrolls up, once they no longer fit; what fits starts at the top.
	// The list reports whether it is before its end after each layout.
	l := &u.entryList
	follow := u.view == viewLive || u.view == viewConsole
	l.ScrollToEnd = follow && l.Position.Length > gtx.Constraints.Max.Y
	if l.ScrollToEnd && u.following {
		l.Position.BeforeEnd = false
	}
	return layout.Stack{Alignment: layout.SE}.Layout(gtx,
		layout.Expanded(func(gtx C) D {
			dims := u.entryScroll.layout(gtx, u.th, l, len(u.entries)+1, func(gtx C, i int) D {
				if i == len(u.entries) {
					return layout.Spacer{Height: 48}.Layout(gtx)
				}
				gtx.Constraints.Min.X = gtx.Constraints.Max.X // so the scroll bar is at the pane's edge
				return layout.Inset{Left: 28, Right: 28}.Layout(gtx, func(gtx C) D { return u.layoutEntry(gtx, i) })
			})
			if l.ScrollToEnd {
				u.following = !l.Position.BeforeEnd
			} else if follow && u.following && l.Position.Length > gtx.Constraints.Max.Y {
				// It no longer fits: move to its end in the next frame
				// rather than at the next poll.
				gtx.Execute(op.InvalidateCmd{})
			}
			return dims
		}),
		layout.Stacked(func(gtx C) D {
			if !l.ScrollToEnd || u.following {
				return D{}
			}
			return layout.UniformInset(20).Layout(gtx, func(gtx C) D {
				return u.button(gtx, &u.latestBtn, "Jump to latest", accent)
			})
		}),
	)
}

func (u *UI) placeholder(gtx C, txt string) D {
	return layout.Inset{Left: 28, Right: 28, Top: 24}.Layout(gtx, u.text(15, muted, txt).Layout)
}

func (u *UI) layoutEntry(gtx C, i int) D {
	e := u.entries[i]
	switch e.kind {
	case entryTurn:
		top := unit.Dp(22)
		if i == 0 {
			top = 18
		}
		return layout.Inset{Top: top, Bottom: 2}.Layout(gtx, func(gtx C) D { return u.layoutTurnHeading(gtx, e.turn) })
	case entryNote:
		c := accent
		if e.bad {
			c = danger
		}
		return layout.Inset{Top: 18, Bottom: 6}.Layout(gtx, func(gtx C) D {
			return leftBar(gtx, c, u.text(15, ink, e.text).Layout)
		})
	case entryConsole:
		return u.layoutConsoleLine(gtx, e)
	}
	return layout.Inset{Left: gutter, Top: 5, Bottom: 5}.Layout(gtx, func(gtx C) D { return u.layoutEvent(gtx, i, e) })
}

// layoutConsoleLine lays out a line of the console as part of a box that
// runs from its first line to its last. Each line is an entry of its own so
// that a long console costs only the lines in view.
func (u *UI) layoutConsoleLine(gtx C, e entry) D {
	var margin, top, bottom unit.Dp
	if e.first {
		margin, top = 12, 10
	}
	if e.last {
		bottom = 10
	}
	return layout.Inset{Top: margin}.Layout(gtx, func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		m := op.Record(gtx.Ops)
		dims := layout.Inset{Left: 12, Right: 12, Top: top, Bottom: bottom}.Layout(gtx, u.code(13, ink, e.text).Layout)
		call := m.Stop()
		r, w := dims.Size, max(1, gtx.Dp(1))
		fillRect(gtx, image.Rectangle{Max: r}, codeBg)
		fillRect(gtx, image.Rect(0, 0, w, r.Y), codeLine)
		fillRect(gtx, image.Rect(r.X-w, 0, r.X, r.Y), codeLine)
		if e.first {
			fillRect(gtx, image.Rect(0, 0, r.X, w), codeLine)
		}
		if e.last {
			fillRect(gtx, image.Rect(0, r.Y-w, r.X, r.Y), codeLine)
		}
		call.Add(gtx.Ops)
		return dims
	})
}

// turnTitle names a turn by its role, or a thinker by its number.
func turnTitle(t session.TurnLog) string {
	if t.Thinker {
		return fmt.Sprintf("Thinker %d", t.Round)
	}
	return roleTitle(t.Role)
}

// turnSubtitle says how a turn went, and in which round.
func (u *UI) turnSubtitle(t session.TurnLog) string {
	_, ok, running := u.turnState(t)
	state := "done"
	switch {
	case running:
		state = "in progress"
	case !ok:
		state = "did not finish"
	}
	if t.Thinker {
		return upperFirst(state)
	}
	return fmt.Sprintf("Round %d · %s", t.Round, state)
}

// layoutTurnHeading lays out a turn's heading, as Slack shows who wrote a
// message: an avatar, the role's name, and the round.
func (u *UI) layoutTurnHeading(gtx C, t session.TurnLog) D {
	mark, ok, running := u.turnState(t)
	markColor := go_
	switch {
	case running:
		markColor = accent
	case !ok:
		markColor = danger
	}
	return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			size := gtx.Dp(avatarSize)
			gtx.Constraints = layout.Exact(image.Pt(size, size))
			defer clip.UniformRRect(image.Rectangle{Max: image.Pt(size, size)}, gtx.Dp(7)).Push(gtx.Ops).Pop()
			paint.Fill(gtx.Ops, roleColors[t.Role])
			return layout.Center.Layout(gtx, u.bold(16, white, turnTitle(t)[:1]).Layout)
		}),
		layout.Rigid(space(12)),
		layout.Rigid(u.bold(15, ink, turnTitle(t)).Layout),
		layout.Rigid(space(8)),
		layout.Rigid(u.text(13, muted, u.turnSubtitle(t)).Layout),
		layout.Rigid(space(6)),
		layout.Rigid(u.symbol(12, markColor, mark).Layout),
	)
}

func (u *UI) layoutEvent(gtx C, i int, e entry) D {
	ev := e.event
	switch ev.Kind {
	case transcript.Info:
		return u.text(13, muted, ev.Title).Layout(gtx)
	case transcript.Message:
		return u.layoutProse(gtx, ev.Text, ink, false)
	case transcript.Thinking:
		return leftBar(gtx, quoteBar, func(gtx C) D { return u.layoutProse(gtx, ev.Text, muted, true) })
	case transcript.Error:
		return leftBar(gtx, danger, u.text(14, danger, strings.TrimSpace(ev.Text)).Layout)
	case transcript.Result:
		mark, c := "✓", go_
		if ev.Failed {
			mark, c = "✗", danger
		}
		children := []layout.FlexChild{layout.Rigid(func(gtx C) D {
			return layout.Flex{}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Dp(18)
					return u.symbol(12, c, mark).Layout(gtx)
				}),
				layout.Flexed(1, u.text(13, muted, ev.Title).Layout),
			)
		})}
		if ev.Failed && strings.TrimSpace(ev.Text) != "" {
			children = append(children, layout.Rigid(u.text(14, danger, strings.TrimSpace(ev.Text)).Layout))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	case transcript.ToolResult:
		return u.layoutOutput(gtx, i, e.key, ev.Text, ev.Failed)
	}

	// Tool calls, commands, and file changes: a line saying what the agent
	// did, then its output or input.
	glyph, c := "▸", accent
	switch ev.Kind {
	case transcript.Command:
		glyph, c = "$", go_
	case transcript.FileChange:
		glyph, c = "✎", changeInk
	}
	if ev.Failed {
		c = danger
	}
	children := []layout.FlexChild{layout.Rigid(func(gtx C) D {
		return layout.Flex{}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Dp(18)
				return u.symbol(13, c, glyph).Layout(gtx)
			}),
			layout.Flexed(1, func(gtx C) D {
				l := u.code(13, ink, ev.Title)
				l.MaxLines = 3
				return l.Layout(gtx)
			}),
		)
	})}
	if body := strings.TrimSpace(ev.Text); body != "" {
		children = append(children, layout.Rigid(space(4)), layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 18}.Layout(gtx, func(gtx C) D {
				if ev.Kind == transcript.ToolCall {
					return u.layoutToolInput(gtx, i, e.key, body)
				}
				return u.layoutOutput(gtx, i, e.key, body, ev.Failed)
			})
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// layoutToolInput shows a tool call's input, which mostly repeats its
// title, only on request.
func (u *UI) layoutToolInput(gtx C, i int, key, body string) D {
	if !u.expanded[key] {
		return u.link(gtx, i, "Show input")
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return boxed(gtx, codeBg, codeLine, 10, u.code(13, ink, body).Layout) }),
		layout.Rigid(func(gtx C) D { return u.link(gtx, i, "Hide input") }),
	)
}

// layoutOutput shows a tool's output, cut to its first lines until it is
// expanded.
func (u *UI) layoutOutput(gtx C, i int, key, body string, failed bool) D {
	body = strings.TrimRight(body, "\n")
	short, more := firstLines(body, maxOutputLines)
	if !u.expanded[key] {
		body = short
	}
	border := codeLine
	if failed {
		border = danger
	}
	children := []layout.FlexChild{layout.Rigid(func(gtx C) D {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X
		return boxed(gtx, codeBg, border, 10, u.code(13, ink, body).Layout)
	})}
	if more > 0 {
		label := fmt.Sprintf("Show %d more lines", more)
		if u.expanded[key] {
			label = "Show less"
		}
		children = append(children, layout.Rigid(func(gtx C) D { return u.link(gtx, i, label) }))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// firstLines returns the first n lines of s, and how many lines follow.
func firstLines(s string, n int) (string, int) {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s, 0
	}
	return strings.Join(lines[:n], "\n"), len(lines) - n
}

// link lays out a clickable label that expands or collapses entry i.
func (u *UI) link(gtx C, i int, txt string) D {
	return layout.Inset{Top: 4}.Layout(gtx, func(gtx C) D {
		return material.Clickable(gtx, &u.expandBtns[i], u.text(13, accent, txt).Layout)
	})
}

// layoutProse lays out text an agent wrote, formatting its Markdown unless
// that is turned off.
func (u *UI) layoutProse(gtx C, src string, c color.NRGBA, italic bool) D {
	src = strings.TrimSpace(src)
	if !u.markdownOn.Value {
		l := u.text(15, c, src)
		if italic {
			l.Font.Style = font.Italic
		}
		return l.Layout(gtx)
	}
	return u.layoutMarkdown(gtx, u.markdown(src), c, italic)
}

func (u *UI) layoutMarkdown(gtx C, blocks []mdBlock, c color.NRGBA, italic bool) D {
	children := make([]layout.FlexChild, 0, 2*len(blocks))
	for i, b := range blocks {
		if i > 0 {
			gap := unit.Dp(10)
			if b.indent > 0 && blocks[i-1].indent > 0 {
				gap = 4
			}
			children = append(children, layout.Rigid(space(gap)))
		}
		children = append(children, layout.Rigid(func(gtx C) D { return u.layoutBlock(gtx, b, c, italic) }))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

func (u *UI) layoutBlock(gtx C, b mdBlock, c color.NRGBA, italic bool) D {
	w := func(gtx C) D {
		switch b.kind {
		case mdHeading:
			size := unit.Sp(16)
			switch b.level {
			case 1:
				size = 20
			case 2:
				size = 18
			}
			return u.rich(gtx, b.spans, size, c, true, italic)
		case mdCode:
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			return boxed(gtx, codeBg, codeLine, 10, u.code(13, ink, b.code).Layout)
		case mdRule:
			return layout.Inset{Top: 4, Bottom: 4}.Layout(gtx, hrule)
		case mdTable:
			return u.layoutTable(gtx, b.rows, c)
		}
		return u.rich(gtx, b.spans, 15, c, false, italic)
	}
	if b.indent > 0 {
		inner := w
		w = func(gtx C) D {
			return layout.Inset{Left: unit.Dp(20 * (b.indent - 1))}.Layout(gtx, func(gtx C) D {
				return layout.Flex{}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						gtx.Constraints.Min.X = gtx.Dp(22)
						return u.text(15, c, b.marker).Layout(gtx)
					}),
					layout.Flexed(1, inner),
				)
			})
		}
	}
	for range b.quote {
		inner := w
		w = func(gtx C) D { return leftBar(gtx, quoteBar, inner) }
	}
	return w(gtx)
}

// rich lays out styled spans of text.
func (u *UI) rich(gtx C, spans []mdSpan, size unit.Sp, c color.NRGBA, bold, italic bool) D {
	if len(spans) == 0 {
		return D{}
	}
	styles := make([]styledtext.SpanStyle, 0, len(spans))
	for _, s := range spans {
		st := styledtext.SpanStyle{Font: font.Font{Typeface: sans}, Size: size, Color: c, Content: s.text}
		if s.bold || bold {
			st.Font.Weight = font.Bold
		}
		if s.italic || italic {
			st.Font.Style = font.Italic
		}
		switch {
		case s.code:
			st.Font = font.Font{Typeface: mono}
			st.Size, st.Color = size-2, codeInk
		case s.link:
			st.Color = accent
		case s.strike:
			st.Color = faint
		}
		styles = append(styles, st)
	}
	t := styledtext.Text(u.th.Shaper, styles...)
	t.LineHeightScale = lineScale
	return t.Layout(gtx, nil)
}

func (u *UI) layoutTable(gtx C, rows [][][]mdSpan, c color.NRGBA) D {
	return widget.Border{Color: line, CornerRadius: 6, Width: 1}.Layout(gtx, func(gtx C) D {
		var children []layout.FlexChild
		for r, row := range rows {
			if r > 0 {
				children = append(children, layout.Rigid(hrule))
			}
			children = append(children, layout.Rigid(func(gtx C) D {
				cells := make([]layout.FlexChild, len(row))
				for i, cell := range row {
					cells[i] = layout.Flexed(1, func(gtx C) D {
						return layout.Inset{Left: 10, Right: 10, Top: 6, Bottom: 6}.Layout(gtx, func(gtx C) D {
							return u.rich(gtx, cell, 14, c, r == 0, false)
						})
					})
				}
				return layout.Flex{}.Layout(gtx, cells...)
			}))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}
