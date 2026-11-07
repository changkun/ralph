package main

import (
	"fmt"
	"image"
	"image/color"
	"strings"

	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

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

var (
	accent    = rgb(0x4f46e5)
	ink       = rgb(0x111827)
	muted     = rgb(0x6b7280)
	faint     = rgb(0x9ca3af)
	line      = rgb(0xe5e7eb)
	panel     = rgb(0xf9fafb)
	selected  = rgb(0xe0e7ff)
	danger    = rgb(0xdc2626)
	success   = rgb(0x16a34a)
	codeBg    = rgb(0xf3f4f6)
	thinkInk  = rgb(0x7c3aed)
	toolInk   = rgb(0x2563eb)
	cmdInk    = rgb(0x0f766e)
	changeInk = rgb(0xb45309)
)

const mono = "Go Mono"

// The widths of the settings and turn columns.
const (
	sidebarWidth = 440
	turnsWidth   = 220
)

// Long tool output is cut to this many lines.
const maxOutputLines = 40

func (u *UI) layout(gtx C) D {
	u.update(gtx)
	paint.Fill(gtx.Ops, rgb(0xffffff))
	dims := layout.Flex{}.Layout(gtx,
		layout.Rigid(func(gtx C) D { return column(gtx, sidebarWidth, panel, u.layoutSidebar) }),
		layout.Rigid(vrule),
		layout.Rigid(func(gtx C) D { return column(gtx, turnsWidth, panel, u.layoutTurns) }),
		layout.Rigid(vrule),
		layout.Flexed(1, u.layoutView),
	)
	u.sync(gtx)
	return dims
}

// column lays out w in a fixed-width column filled with bg.
func column(gtx C, width unit.Dp, bg color.NRGBA, w layout.Widget) D {
	px := gtx.Dp(width)
	gtx.Constraints.Min.X, gtx.Constraints.Max.X = px, px
	gtx.Constraints.Min.Y = gtx.Constraints.Max.Y
	return layout.Background{}.Layout(gtx, func(gtx C) D { return fill(gtx, bg) }, w)
}

func fill(gtx C, c color.NRGBA) D {
	defer clip.Rect{Max: gtx.Constraints.Min}.Push(gtx.Ops).Pop()
	paint.ColorOp{Color: c}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	return D{Size: gtx.Constraints.Min}
}

func vrule(gtx C) D {
	size := image.Pt(gtx.Dp(1), gtx.Constraints.Max.Y)
	defer clip.Rect{Max: size}.Push(gtx.Ops).Pop()
	paint.ColorOp{Color: line}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	return D{Size: size}
}

func hrule(gtx C) D {
	size := image.Pt(gtx.Constraints.Max.X, gtx.Dp(1))
	defer clip.Rect{Max: size}.Push(gtx.Ops).Pop()
	paint.ColorOp{Color: line}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	return D{Size: size}
}

func space(dp unit.Dp) layout.Widget {
	return func(gtx C) D { return layout.Spacer{Height: dp, Width: dp}.Layout(gtx) }
}

func (u *UI) label(size unit.Sp, c color.NRGBA, txt string) material.LabelStyle {
	l := material.Label(u.th, size, txt)
	l.Color = c
	return l
}

func (u *UI) heading(txt string) layout.Widget {
	return func(gtx C) D {
		return layout.Inset{Top: 18, Bottom: 6}.Layout(gtx, func(gtx C) D {
			l := u.label(11, muted, strings.ToUpper(txt))
			l.Font.Weight = font.Bold
			return l.Layout(gtx)
		})
	}
}

// box draws an outlined, padded box around an editor.
func (u *UI) box(gtx C, ed *widget.Editor, hint string, mono bool) D {
	return widget.Border{Color: line, CornerRadius: 4, Width: 1}.Layout(gtx, func(gtx C) D {
		return layout.Background{}.Layout(gtx,
			func(gtx C) D { return fill(gtx, rgb(0xffffff)) },
			func(gtx C) D {
				return layout.UniformInset(6).Layout(gtx, func(gtx C) D {
					gtx.Constraints.Min.X = gtx.Constraints.Max.X
					e := material.Editor(u.th, ed, hint)
					if mono {
						e.Font.Typeface = font.Typeface("Go Mono")
						e.TextSize = 12
					}
					return e.Layout(gtx)
				})
			})
	})
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
		func(gtx C) D {
			l := u.label(22, ink, "Ralph")
			l.Font.Weight = font.Bold
			return l.Layout(gtx)
		},
		func(gtx C) D {
			return u.label(12, muted, "Runs coding agents in rounds on a project, committing it after each round.").Layout(gtx)
		},
		u.heading("Project folder"),
		func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, u.enabled(func(gtx C) D { return u.box(gtx, &u.folderEd, "Path, or choose a folder", false) })),
				layout.Rigid(space(8)),
				layout.Rigid(u.enabled(func(gtx C) D {
					b := material.Button(u.th, &u.chooseBtn, "Choose…")
					return b.Layout(gtx)
				})),
			)
		},
		u.layoutControls,
		u.heading("Runtime"),
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
			for _, p := range loop.Patterns {
				children = append(children, layout.Rigid(material.RadioButton(u.th, &u.pattern, string(p), string(p)).Layout))
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		}),
		u.heading("Rounds"),
		u.enabled(func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					gtx.Constraints.Max.X = gtx.Dp(90)
					return u.box(gtx, &u.rounds, "no limit", false)
				}),
				layout.Rigid(space(8)),
				layout.Rigid(u.label(12, muted, "Stop after this round. Empty means no limit.").Layout),
			)
		}),
	}
	pattern := loop.Pattern(u.pattern.Value)
	for _, role := range pattern.Roles() {
		items = append(items, u.heading(roleTitle(role)), u.enabled(func(gtx C) D { return u.layoutRole(gtx, role) }))
	}
	items = append(items, space(24))
	return material.List(u.th, &u.sidebar).Layout(gtx, len(items), func(gtx C, i int) D {
		return layout.Inset{Left: 20, Right: 20, Top: firstTop(i)}.Layout(gtx, items[i])
	})
}

func firstTop(i int) unit.Dp {
	if i == 0 {
		return 20
	}
	return 0
}

// layoutControls lays out the Start and Stop buttons, the session's state,
// and any problem.
func (u *UI) layoutControls(gtx C) D {
	active := u.active()
	status, bad := u.statusText()
	statusColor := muted
	if bad {
		statusColor = danger
	}
	children := []layout.FlexChild{
		layout.Rigid(space(16)),
		layout.Rigid(func(gtx C) D {
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx C) D {
					if active || u.folder == "" {
						gtx = gtx.Disabled()
					}
					b := material.Button(u.th, &u.startBtn, "Start")
					return b.Layout(gtx)
				}),
				layout.Rigid(space(8)),
				layout.Rigid(func(gtx C) D {
					if !active {
						gtx = gtx.Disabled()
					}
					b := material.Button(u.th, &u.stopBtn, "Stop")
					b.Background = danger
					return b.Layout(gtx)
				}),
			)
		}),
		layout.Rigid(space(8)),
		layout.Rigid(u.label(13, statusColor, status).Layout),
	}
	for _, problem := range []string{u.message, errText(u.invalid)} {
		if problem != "" {
			children = append(children, layout.Rigid(space(6)), layout.Rigid(u.label(13, danger, problem).Layout))
		}
	}
	if active {
		children = append(children, layout.Rigid(space(6)),
			layout.Rigid(u.label(12, faint, "Settings can be changed once the session stops.").Layout))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (u *UI) layoutLogins(gtx C) D {
	if u.runtime.Value == session.Host {
		return layout.Inset{Top: 4}.Layout(gtx,
			u.label(12, muted, "Agents run directly on this machine, with the CLIs' own logins and permissions.").Layout)
	}
	var children []layout.FlexChild
	children = append(children, layout.Rigid(
		u.label(12, muted, "Agents run in a new container that sees only the project folder.").Layout))
	for _, l := range u.logins {
		txt, c := fmt.Sprintf("%s: logs in with %s", l.Backend, strings.Join(l.Found, ", ")), success
		if len(l.Found) == 0 {
			txt, c = fmt.Sprintf("%s: no login found; %s", l.Backend, l.Help), danger
		}
		children = append(children, layout.Rigid(space(4)), layout.Rigid(u.label(12, c, txt).Layout))
	}
	return layout.Inset{Top: 4}.Layout(gtx, func(gtx C) D {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

func (u *UI) layoutRole(gtx C, role backend.Role) D {
	f := u.roles[role]
	children := []layout.FlexChild{
		layout.Rigid(u.label(12, muted, roleInfo[role]).Layout),
		layout.Rigid(space(6)),
		layout.Rigid(func(gtx C) D {
			var radios []layout.FlexChild
			for _, name := range backend.Names {
				radios = append(radios, layout.Rigid(material.RadioButton(u.th, &f.backend, name, name).Layout))
			}
			radios = append(radios, layout.Rigid(space(8)), layout.Flexed(1, func(gtx C) D {
				return u.box(gtx, &f.model, "model (CLI default)", false)
			}))
			return layout.Flex{Alignment: layout.Middle}.Layout(gtx, radios...)
		}),
		layout.Rigid(material.CheckBox(u.th, &f.edit, "Edit prompts").Layout),
	}
	if f.edit.Value {
		children = append(children,
			layout.Rigid(u.label(12, muted, "System prompt").Layout),
			layout.Rigid(space(4)),
			layout.Rigid(func(gtx C) D { return u.box(gtx, &f.system, "none", true) }),
			layout.Rigid(space(8)),
			layout.Rigid(u.label(12, muted, "Prompt").Layout),
			layout.Rigid(space(4)),
			layout.Rigid(func(gtx C) D { return u.box(gtx, &f.prompt, "", true) }),
			layout.Rigid(space(8)),
			layout.Rigid(func(gtx C) D {
				b := material.Button(u.th, &f.reset, "Reset to the built-in prompts")
				b.Background, b.Color = rgb(0xffffff), accent
				b.TextSize = 12
				return widget.Border{Color: line, CornerRadius: 4, Width: 1}.Layout(gtx, b.Layout)
			}),
		)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

func (u *UI) layoutTurns(gtx C) D {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 12, Right: 12, Top: 16, Bottom: 8}.Layout(gtx,
				material.CheckBox(u.th, &u.follow, "Follow the session").Layout)
		}),
		layout.Rigid(hrule),
		layout.Flexed(1, func(gtx C) D {
			if u.folder == "" {
				return D{}
			}
			return material.List(u.th, &u.turnList).Layout(gtx, len(u.turns)+1, u.layoutTurnItem)
		}),
	)
}

func (u *UI) layoutTurnItem(gtx C, i int) D {
	title, mark, markColor := "Engine log", "", muted
	var header string
	if i > 0 {
		t := u.turns[i-1]
		title = roleTitle(t.Role)
		switch {
		case t.Done:
			mark, markColor = "✓", success
		case i == len(u.turns) && u.active():
			mark, markColor = "…", accent
		default:
			mark, markColor = "✗", danger
		}
		if i == 1 || u.turns[i-2].Round != t.Round {
			header = fmt.Sprintf("Round %d", t.Round)
		}
	}
	item := func(gtx C) D {
		return material.Clickable(gtx, &u.turnBtns[i], func(gtx C) D {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			bg := color.NRGBA{}
			if i == u.view {
				bg = selected
			}
			return layout.Background{}.Layout(gtx,
				func(gtx C) D { return fill(gtx, bg) },
				func(gtx C) D {
					return layout.Inset{Left: 20, Right: 12, Top: 6, Bottom: 6}.Layout(gtx, func(gtx C) D {
						return layout.Flex{Alignment: layout.Middle}.Layout(gtx,
							layout.Flexed(1, u.label(13, ink, title).Layout),
							layout.Rigid(u.label(13, markColor, mark).Layout),
						)
					})
				})
		})
	}
	if header == "" {
		return item(gtx)
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 12, Top: 12, Bottom: 2}.Layout(gtx, func(gtx C) D {
				l := u.label(11, muted, strings.ToUpper(header))
				l.Font.Weight = font.Bold
				return l.Layout(gtx)
			})
		}),
		layout.Rigid(item),
	)
}

func (u *UI) layoutView(gtx C) D {
	title := "Engine log"
	if u.view > 0 && u.view <= len(u.turns) {
		t := u.turns[u.view-1]
		title = fmt.Sprintf("Round %d · %s", t.Round, roleTitle(t.Role))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 24, Right: 24, Top: 16, Bottom: 12}.Layout(gtx, func(gtx C) D {
				l := u.label(18, ink, title)
				l.Font.Weight = font.Bold
				return l.Layout(gtx)
			})
		}),
		layout.Rigid(hrule),
		layout.Flexed(1, func(gtx C) D {
			// Keep following the end of the view once it no longer fits,
			// but start short content at the top.
			u.eventList.ScrollToEnd = u.eventList.Position.Length > gtx.Constraints.Max.Y
			if u.view != u.shownView {
				u.shownView, u.eventList.Position = u.view, layout.Position{}
			}
			switch {
			case u.folder == "":
				return u.placeholder(gtx, "Choose a project folder. Ralph makes it a git repository if it is not one, and keeps its logs in .ralph.")
			case u.view == 0:
				return u.layoutLog(gtx)
			case len(u.events) == 0:
				return u.placeholder(gtx, "Waiting for the agent…")
			}
			return material.List(u.th, &u.eventList).Layout(gtx, len(u.events), func(gtx C, i int) D {
				return layout.Inset{Left: 24, Right: 24, Top: 10, Bottom: 6}.Layout(gtx, func(gtx C) D {
					return u.layoutEvent(gtx, u.events[i])
				})
			})
		}),
	)
}

func (u *UI) placeholder(gtx C, txt string) D {
	return layout.UniformInset(24).Layout(gtx, u.label(14, muted, txt).Layout)
}

func (u *UI) layoutLog(gtx C) D {
	if len(u.logLines) == 0 || len(u.logLines) == 1 && u.logLines[0] == "" {
		return u.placeholder(gtx, "No session has run in this folder yet.")
	}
	return material.List(u.th, &u.eventList).Layout(gtx, len(u.logLines), func(gtx C, i int) D {
		return layout.Inset{Left: 24, Right: 24, Top: firstTop(i) / 2}.Layout(gtx, func(gtx C) D {
			l := u.label(12, ink, u.logLines[i])
			l.Font.Typeface = mono
			return l.Layout(gtx)
		})
	})
}

// eventStyles gives each kind of event a tag and a color.
var eventStyles = map[transcript.Kind]struct {
	tag   string
	color color.NRGBA
}{
	transcript.Info:       {"info", muted},
	transcript.Message:    {"agent", ink},
	transcript.Thinking:   {"thinking", thinkInk},
	transcript.ToolCall:   {"tool", toolInk},
	transcript.ToolResult: {"output", muted},
	transcript.Command:    {"command", cmdInk},
	transcript.FileChange: {"edit", changeInk},
	transcript.Error:      {"error", danger},
	transcript.Result:     {"done", success},
}

func (u *UI) layoutEvent(gtx C, e transcript.Event) D {
	style := eventStyles[e.Kind]
	tag, tagColor := style.tag, style.color
	if e.Failed {
		tagColor = danger
		if e.Kind == transcript.Result {
			tag = "failed"
		}
	}
	var children []layout.FlexChild
	children = append(children, layout.Rigid(func(gtx C) D {
		return layout.Flex{Alignment: layout.Baseline}.Layout(gtx,
			layout.Rigid(func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Dp(76)
				l := u.label(11, tagColor, strings.ToUpper(tag))
				l.Font.Weight = font.Bold
				return l.Layout(gtx)
			}),
			layout.Flexed(1, func(gtx C) D {
				if e.Title == "" {
					return D{}
				}
				l := u.label(14, ink, e.Title)
				if e.Kind == transcript.Command || e.Kind == transcript.ToolCall {
					l.Font.Typeface = mono
					l.TextSize = 13
				}
				l.MaxLines = 3
				return l.Layout(gtx)
			}),
		)
	}))
	body := strings.TrimSpace(e.Text)
	if e.Kind == transcript.Result && !e.Failed {
		body = "" // the final answer, which the last message already shows
	}
	if body != "" {
		children = append(children, layout.Rigid(space(4)), layout.Rigid(func(gtx C) D {
			return layout.Inset{Left: 76}.Layout(gtx, func(gtx C) D { return u.layoutBody(gtx, e.Kind, body) })
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

func (u *UI) layoutBody(gtx C, kind transcript.Kind, body string) D {
	switch kind {
	case transcript.Message:
		return u.label(14, ink, body).Layout(gtx)
	case transcript.Thinking:
		l := u.label(13, muted, body)
		l.Font.Style = font.Italic
		return l.Layout(gtx)
	case transcript.Error:
		return u.label(13, danger, body).Layout(gtx)
	}
	body = clipLines(body, maxOutputLines)
	return layout.Background{}.Layout(gtx,
		func(gtx C) D { return fill(gtx, codeBg) },
		func(gtx C) D {
			return layout.UniformInset(8).Layout(gtx, func(gtx C) D {
				gtx.Constraints.Min.X = gtx.Constraints.Max.X
				l := u.label(12, ink, body)
				l.Font.Typeface = mono
				l.WrapPolicy = text.WrapGraphemes
				return l.Layout(gtx)
			})
		})
}

// clipLines cuts s to its first max lines.
func clipLines(s string, max int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= max {
		return s
	}
	return strings.Join(lines[:max], "\n") + fmt.Sprintf("\n… %d more lines", len(lines)-max)
}
