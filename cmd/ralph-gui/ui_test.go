package main

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"gioui.org/f32"
	"gioui.org/gpu/headless"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/x/explorer"

	"github.com/changkun/ralph/cmd/ralph-gui/internal/trackpad"
	"github.com/changkun/ralph/internal/backend"
	"github.com/changkun/ralph/internal/git"
	"github.com/changkun/ralph/internal/loop"
	"github.com/changkun/ralph/internal/session"
)

// TestMain keeps the tests' preferences out of the user's.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "ralph-gui")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", home)
	os.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	os.Setenv("AppData", home)
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

const width, height = 1360, 860

// frame lays out one frame of u and returns its operations.
func frame(t *testing.T, u *UI) *op.Ops {
	t.Helper()
	return frameAt(t, u, time.Now())
}

func frameAt(t *testing.T, u *UI, now time.Time) *op.Ops {
	t.Helper()
	ops := new(op.Ops)
	r := u.testRouter()
	gtx := layout.Context{
		Source:      r.Source(),
		Ops:         ops,
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(width, height)),
		Now:         now,
	}
	u.layout(gtx)
	r.Frame(ops)
	return ops
}

var routers = map[*UI]*input.Router{}

// testRouter returns the input router of u's test frames.
func (u *UI) testRouter() *input.Router {
	if routers[u] == nil {
		routers[u] = new(input.Router)
	}
	return routers[u]
}

// scroll scrolls the mouse wheel at (x, y) by dy pixels.
func scroll(u *UI, x, y, dy float32) {
	u.testRouter().Queue(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: f32.Pt(x, y), Scroll: f32.Pt(0, dy)})
}

// screenshot saves a picture of u's next frame if RALPH_GUI_SCREENSHOTS
// names a folder.
func screenshot(t *testing.T, u *UI, name string) {
	t.Helper()
	dir := os.Getenv("RALPH_GUI_SCREENSHOTS")
	if dir == "" {
		return
	}
	w, err := headless.NewWindow(width, height)
	if err != nil {
		t.Skipf("no headless GPU: %v", err)
	}
	defer w.Release()
	if err := w.Frame(frame(t, u)); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rectangle{Max: w.Size()})
	if err := w.Screenshot(img); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(dir, name+".png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	write(t, dst, string(data))
}

// repo returns a new git repository.
func repo(t *testing.T) string {
	t.Helper()
	folder := t.TempDir()
	if err := git.Init(folder); err != nil {
		t.Fatal(err)
	}
	return folder
}

// freshPrefs removes the saved preferences.
func freshPrefs(t *testing.T) {
	t.Helper()
	if err := os.Remove(prefsPath()); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

// project returns a folder with the logs of a session that finished two
// turns and failed in a third.
func project(t *testing.T) string {
	t.Helper()
	folder := repo(t)
	logs := filepath.Join(folder, ".ralph")
	testdata := filepath.Join("..", "..", "internal", "transcript", "testdata")
	copyFile(t, filepath.Join(testdata, "codex.jsonl"), filepath.Join(logs, "round-001-strategist.jsonl"))
	write(t, filepath.Join(logs, "round-001-strategist.json"), `{"result":"done"}`)
	copyFile(t, filepath.Join(testdata, "claude.jsonl"), filepath.Join(logs, "round-001-executor.jsonl"))
	write(t, filepath.Join(logs, "round-001-executor.json"), `{"result":"done"}`)
	copyFile(t, filepath.Join(testdata, "codex-failed.jsonl"), filepath.Join(logs, "round-002-strategist.jsonl"))
	write(t, filepath.Join(folder, session.LocalDir, "status.json"),
		`{"pid":1,"state":"failed","pattern":"think+act","runtime":"podman","round":2,"error":"strategist: codex: not supported"}`)
	write(t, filepath.Join(folder, session.LocalDir, "engine.log"), "=== Ralph loop starting ===\nFolder: "+folder+"\n")
	return folder
}

func kinds(es []entry) string {
	var b strings.Builder
	for _, e := range es {
		b.WriteByte("TENC"[e.kind])
	}
	return b.String()
}

// frameUntil lays out frames of u until done reports true.
func frameUntil(t *testing.T, u *UI, done func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); !done(); time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		frame(t, u)
	}
}

func TestNoFolder(t *testing.T) {
	freshPrefs(t)
	u := newUI(nil, "")
	// A folder picker that answers what the test tells it to.
	asked, answers := make(chan bool, 2), make(chan chosenFolder)
	u.pick = func() (string, error) {
		asked <- true
		c := <-answers
		return c.path, c.err
	}
	frame(t, u)
	select {
	case <-asked:
	case <-time.After(5 * time.Second):
		t.Fatal("the app did not ask for a folder")
	}
	if s, _ := u.statusText(); !strings.Contains(s, "Choose a project folder") {
		t.Errorf("status = %q", s)
	}
	u.start()
	if u.message == "" {
		t.Error("Start without a folder did nothing to say so")
	}
	screenshot(t, u, "empty")

	// Declining leaves the app without a folder, and it asks no more.
	answers <- chosenFolder{err: explorer.ErrUserDecline}
	frameUntil(t, u, func() bool { return !u.choosing })
	frame(t, u)
	if u.folder != "" || len(asked) != 0 {
		t.Errorf("folder = %q, asked again: %v", u.folder, len(asked) != 0)
	}

	// Choose… asks again, and the folder chosen opens.
	folder := repo(t)
	u.chooseFolder()
	<-asked
	answers <- chosenFolder{path: folder}
	frameUntil(t, u, func() bool { return u.folder == folder })

	// The app reopens the folder, so it need not ask.
	if u := newUI(nil, ""); u.folder != folder || u.ask {
		t.Errorf("folder = %q, ask = %v; want %q, false", u.folder, u.ask, folder)
	}
}

func TestViews(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "x")
	folder := project(t)
	u := newUI(nil, folder)
	if len(u.turns) != 3 {
		t.Fatalf("turns = %+v", u.turns)
	}

	// The live view shows every turn, then why the session failed.
	if u.view != viewLive {
		t.Errorf("view = %d, want the live view", u.view)
	}
	if k := kinds(u.entries); k != "TEEEETEEEEETEEEN" {
		t.Errorf("live entries = %s", k)
	}
	if last := u.entries[len(u.entries)-1]; !last.bad || !strings.Contains(last.text, "not supported") {
		t.Errorf("last entry = %+v", last)
	}

	// Starting again continues after the round that finished.
	if s := u.roundsText(); u.finished != 1 || s != "Runs until you stop it." {
		t.Errorf("finished = %d, rounds = %q", u.finished, s)
	}
	u.rounds.SetText("3")
	if s := u.roundsText(); s != "Runs rounds 2 to 4, then stops." {
		t.Errorf("rounds = %q", s)
	}
	u.rounds.SetText("")
	frame(t, u)
	screenshot(t, u, "live")

	// The console shows the engine's output.
	u.showView(viewConsole)
	u.poll()
	if k := kinds(u.entries); k != "CC" || !strings.Contains(u.entries[0].text, "Ralph loop starting") {
		t.Errorf("console entries = %s %+v", k, u.entries)
	}
	frame(t, u)
	screenshot(t, u, "console")

	// A turn shows its own transcript.
	u.showView(firstTurnView)
	u.poll()
	if k := kinds(u.entries); k != "TEEEE" {
		t.Errorf("turn entries = %s", k)
	}
	for _, f := range u.roles {
		f.edit.Value = true
	}
	frame(t, u)
	screenshot(t, u, "turn")

	// Without Markdown, text is shown as written.
	u.markdownOn.Value = false
	u.showView(firstTurnView + 1)
	u.poll()
	frame(t, u)
	screenshot(t, u, "plain")
	u.markdownOn.Value = true

	// A running session's latest turn is in progress, in the live view.
	write(t, filepath.Join(folder, session.LocalDir, "status.json"),
		`{"pid":`+strconv.Itoa(os.Getpid())+`,"state":"running","round":2,"until":3,"role":"strategist"}`)
	u.showView(viewLive)
	u.poll()
	if !u.active() {
		t.Error("the running session is not active")
	}
	if _, _, running := u.turnState(u.turns[2]); !running {
		t.Error("the latest turn is not in progress")
	}
	if s, _ := u.statusText(); s != "Running round 2 of 3: Strategist." {
		t.Errorf("status = %q", s)
	}
	if k := kinds(u.entries); strings.HasSuffix(k, "N") {
		t.Errorf("live entries = %s; want no note while running", k)
	}
	frame(t, u)
	screenshot(t, u, "running")

	// Another folder can be opened while the session runs, and reopening
	// the session's folder follows it again.
	other := t.TempDir()
	u.openFolder(other)
	if u.folder != other || u.active() {
		t.Errorf("folder = %q, active = %v; want the other, idle folder", u.folder, u.active())
	}
	u.openFolder(folder)
	if !u.active() || u.view != viewLive {
		t.Errorf("active = %v, view = %d; want to follow the running session again", u.active(), u.view)
	}
	if p := loadPrefs(); p.Folder != folder {
		t.Errorf("remembered folder = %q, want %q", p.Folder, folder)
	}
}

func TestFollow(t *testing.T) {
	folder := project(t)
	var log strings.Builder
	for i := range 400 {
		log.WriteString("line " + strconv.Itoa(i) + "\n")
	}
	write(t, filepath.Join(folder, session.LocalDir, "engine.log"), log.String())
	u := newUI(nil, folder)
	u.showView(viewConsole)
	u.poll()
	if len(u.entries) != 400 || !u.entries[0].first || !u.entries[399].last {
		t.Fatalf("console entries = %d; want one per line", len(u.entries))
	}
	// The scroller takes scrolling once a frame has placed it.
	now := time.Now()
	for range 3 {
		frameAt(t, u, now)
	}
	l := &u.entryList
	atTop := func() bool { return l.Position.First == 0 && l.Position.Offset == 0 }
	if !u.following || l.Position.BeforeEnd || atTop() {
		t.Fatalf("following = %v, position = %+v; want the end", u.following, l.Position)
	}
	screenshot(t, u, "follow")

	// The view stays at the end as the session prints more.
	first := l.Position.First
	log.WriteString("more\nand more\n")
	write(t, filepath.Join(folder, session.LocalDir, "engine.log"), log.String())
	u.poll()
	frameAt(t, u, now)
	if !u.following || l.Position.BeforeEnd || l.Position.First <= first {
		t.Errorf("following = %v, position = %+v; want the new end, past line %d", u.following, l.Position, first)
	}

	// Scrolling up stops following.
	scroll(u, 1000, 500, -300)
	frameAt(t, u, now)
	if u.following || !l.Position.BeforeEnd {
		t.Errorf("following = %v, position = %+v; want to stop following", u.following, l.Position)
	}

	// Scrolling back to the end follows again. A mouse wheel stops there.
	s := &u.entryScroll
	scroll(u, 1000, 500, 1000)
	frameAt(t, u, now)
	if !u.following || l.Position.BeforeEnd || s.y != 0 {
		t.Errorf("following = %v, position = %+v, stretch = %v; want to follow", u.following, l.Position, s.y)
	}
	scroll(u, 1000, 500, 100)
	frameAt(t, u, now)
	if s.y != 0 {
		t.Errorf("stretch = %v; want a mouse wheel to stop at the end", s.y)
	}

	// Fingers on a trackpad stretch the content past the end, less and
	// less, and hold it there.
	tp := fakeTrackpad(t, u)
	tp.swipe(100)
	frameAt(t, u, now)
	y := s.y
	if y <= 0 || y >= 100 {
		t.Errorf("stretch = %v; want less than the 100 scrolled", y)
	}
	tp.swipe(100)
	frameAt(t, u, now)
	if s.y <= y || s.y-y >= y {
		t.Errorf("stretch = %v after %v; want it a little further", s.y, y)
	}
	now = now.Add(time.Second)
	frameAt(t, u, now)
	if s.y == 0 {
		t.Error("the stretch sprang back while the fingers held it")
	}

	// Moving back first undoes the stretch.
	position, y := l.Position, s.y
	tp.swipe(-20)
	frameAt(t, u, now)
	if s.y >= y || l.Position.First != position.First || l.Position.Offset != position.Offset {
		t.Errorf("stretch = %v after %v, position = %+v; want less stretch, the list still", s.y, y, l.Position)
	}

	// Once the fingers lift, the stretch springs back, and their momentum
	// is ignored.
	tp.lift()
	position = l.Position
	for i := range 40 {
		if i < 10 {
			tp.coast(50)
		}
		frameAt(t, u, now.Add(time.Duration(i)*16*time.Millisecond))
	}
	if s.y != 0 || s.v != 0 || l.Position.First != position.First || l.Position.Offset != position.Offset {
		t.Errorf("stretch = %v, velocity = %v, position = %+v; want them back at 0, the list still", s.y, s.v, l.Position)
	}

	// Momentum that reaches the end bounces off it, and the rest of it is
	// ignored.
	now = now.Add(2 * time.Second)
	scroll(u, 1000, 500, -100)
	tp.swipe(30)
	tp.lift()
	frameAt(t, u, now)
	peak := float32(0)
	for i := 1; i <= 40; i++ {
		if i <= 10 {
			tp.coast(40)
		}
		frameAt(t, u, now.Add(time.Duration(i)*16*time.Millisecond))
		peak = max(peak, s.y)
	}
	if peak <= 0 || peak > 100 || s.y != 0 || !u.following {
		t.Errorf("peak = %v, stretch = %v, following = %v; want a bounce that settled at the end", peak, s.y, u.following)
	}

	// Short content starts at the top.
	now = now.Add(time.Second)
	write(t, filepath.Join(folder, session.LocalDir, "engine.log"), "one line\n")
	u.poll()
	for range 2 {
		frameAt(t, u, now)
	}
	if l.ScrollToEnd || !atTop() {
		t.Errorf("scroll to end = %v, position = %+v; want the top", l.ScrollToEnd, l.Position)
	}

	// Pulling it down, even from below the content, stretches it.
	tp.at = f32.Pt(1000, 600)
	tp.swipe(-100)
	frameAt(t, u, now)
	if s.y >= 0 {
		t.Errorf("stretch = %v; want the content pulled down", s.y)
	}
	tp.lift()

	// Once it no longer fits, the view moves to its end.
	write(t, filepath.Join(folder, session.LocalDir, "engine.log"), "one line\n"+log.String())
	u.poll()
	for i := range 40 {
		frameAt(t, u, now.Add(time.Duration(i)*16*time.Millisecond))
	}
	if !u.following || l.Position.BeforeEnd || atTop() || s.y != 0 {
		t.Errorf("following = %v, position = %+v, stretch = %v; want the end", u.following, l.Position, s.y)
	}
}

// fakePad scrolls a UI as fingers on a trackpad do.
type fakePad struct {
	u       *UI
	at      f32.Point
	time    time.Duration
	down    bool
	gesture uint64
	phases  map[time.Duration]trackpad.Event
}

// fakeTrackpad makes scroll events carry the phases that a real trackpad
// gives them.
func fakeTrackpad(t *testing.T, u *UI) *fakePad {
	tp := &fakePad{u: u, at: f32.Pt(1000, 500), time: time.Second, phases: map[time.Duration]trackpad.Event{}}
	phaseOf, gesture := scrollPhaseOf, scrollGesture
	t.Cleanup(func() { scrollPhaseOf, scrollGesture = phaseOf, gesture })
	scrollPhaseOf = func(t time.Duration) (trackpad.Event, bool) {
		e, ok := tp.phases[t]
		return e, ok
	}
	scrollGesture = func() (bool, uint64) { return tp.down, tp.gesture }
	return tp
}

// swipe moves the fingers dy pixels, putting them down first if they are
// up.
func (tp *fakePad) swipe(dy float32) {
	ph := trackpad.Changed
	if !tp.down {
		tp.down, ph = true, trackpad.Began
		tp.gesture++
	}
	tp.scroll(dy, trackpad.Event{Finger: ph, Gesture: tp.gesture})
}

// lift lifts the fingers. The event saying so scrolls nothing, and Gio
// drops it.
func (tp *fakePad) lift() { tp.down = false }

// coast scrolls dy pixels with the momentum of the latest gesture.
func (tp *fakePad) coast(dy float32) {
	tp.scroll(dy, trackpad.Event{Momentum: trackpad.Changed, Gesture: tp.gesture})
}

func (tp *fakePad) scroll(dy float32, e trackpad.Event) {
	tp.time += 16 * time.Millisecond
	tp.phases[tp.time] = e
	tp.u.testRouter().Queue(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: tp.at, Scroll: f32.Pt(0, dy), Time: tp.time})
}

// TestScrollIndicator drags the scroll indicator, whose place comes from
// the measured heights of all elements.
func TestScrollIndicator(t *testing.T) {
	folder := project(t)
	var log strings.Builder
	for i := range 400 {
		log.WriteString("line " + strconv.Itoa(i) + "\n")
	}
	write(t, filepath.Join(folder, session.LocalDir, "engine.log"), log.String())
	u := newUI(nil, folder)
	u.showView(viewConsole)
	u.poll()
	// The scroller measures elements for a few milliseconds a frame, so
	// how many frames it takes depends on the machine.
	s, l := &u.entryScroll, &u.entryList
	measured := func() bool { return len(s.sizes) > 0 && !slices.Contains(s.sizes, -1) }
	now := time.Now()
	for i := 0; !measured(); i++ {
		if i == 100 {
			t.Fatal("elements out of view are not measured")
		}
		frameAt(t, u, now)
	}

	// At the end, the indicator is at the bottom of the pane. Dragging it
	// to the top shows the first line.
	x := float32(width - 4)
	u.testRouter().Queue(
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: f32.Pt(x, height-20)},
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: f32.Pt(x, 0)},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: f32.Pt(x, 0)},
	)
	for range 2 {
		frameAt(t, u, now)
	}
	if u.following || l.Position.First != 0 || l.Position.Offset != 0 {
		t.Errorf("following = %v, position = %+v; want the top", u.following, l.Position)
	}
}

func TestStretch(t *testing.T) {
	for _, x := range []float32{-500, -1, 0, 1, 50, 300, 5000} {
		y := stretch(x, 800)
		if abs32(y) > abs32(x) || abs32(y) >= 800 || (x != 0 && (y > 0) != (x > 0)) {
			t.Errorf("stretch(%v) = %v", x, y)
		}
		if back := unstretch(y, 800); abs32(back-x) > 0.01*max(1, abs32(x)) {
			t.Errorf("unstretch(stretch(%v)) = %v", x, back)
		}
	}
}

func TestResizeColumns(t *testing.T) {
	freshPrefs(t)
	u := newUI(nil, t.TempDir())
	frame(t, u)
	if u.prefs.SidebarWidth != 420 {
		t.Fatalf("sidebar width = %v", u.prefs.SidebarWidth)
	}
	// Drag the handle right of the sidebar 50 pixels to the right.
	r := u.testRouter()
	at := func(x float32) f32.Point { return f32.Pt(x, 400) }
	r.Queue(
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: at(423)},
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Buttons: pointer.ButtonPrimary, Position: at(473)},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: at(473)},
	)
	frame(t, u)
	if u.prefs.SidebarWidth != 470 {
		t.Errorf("sidebar width = %v, want 470", u.prefs.SidebarWidth)
	}
	if p := loadPrefs(); p.SidebarWidth != 470 {
		t.Errorf("saved sidebar width = %v, want 470", p.SidebarWidth)
	}

	// Widths stay within limits that leave room for the other columns.
	u.prefs.SidebarWidth, u.prefs.TurnsWidth = 5000, 10
	frame(t, u)
	if u.prefs.SidebarWidth != 720 || u.prefs.TurnsWidth != 170 {
		t.Errorf("widths = %v, %v; want 720, 170", u.prefs.SidebarWidth, u.prefs.TurnsWidth)
	}
	screenshot(t, u, "wide")
}

func TestPrefs(t *testing.T) {
	freshPrefs(t)
	if p := loadPrefs(); p != defaultPrefs() {
		t.Errorf("prefs = %+v, want the defaults", p)
	}
	p := prefs{SidebarWidth: 380, TurnsWidth: 200, PlainText: true, Folder: "/x"}
	p.save()
	if got := loadPrefs(); got != p {
		t.Errorf("prefs = %+v, want %+v", got, p)
	}
	if u := newUI(nil, ""); u.markdownOn.Value || u.folder != "" {
		t.Errorf("markdown = %v, folder = %q; want plain text and no folder", u.markdownOn.Value, u.folder)
	}
	write(t, prefsPath(), "{")
	if p := loadPrefs(); p != defaultPrefs() {
		t.Errorf("prefs = %+v, want the defaults", p)
	}
}

func TestSettings(t *testing.T) {
	folder := repo(t)
	u := newUI(nil, folder)
	frame(t, u)

	// Changes in the form are checked.
	u.pattern.Value = string(loop.ThinkActEvaluatorArchivist)
	u.runtime.Value = session.Host
	u.rounds.SetText("3")
	f := u.roles[backend.Executor]
	f.backend.Value = "codex"
	f.model.SetText(" gpt-5 ")
	f.prompt.SetText("{{.Nope}}")
	frame(t, u)
	if u.settings.Pattern != loop.ThinkActEvaluatorArchivist || u.settings.Rounds != 3 || u.settings.Agents[backend.Executor].Model != "gpt-5" {
		t.Errorf("settings = %+v", u.settings)
	}
	if u.invalid == nil || !strings.Contains(u.invalid.Error(), "executor prompt") {
		t.Errorf("invalid = %v", u.invalid)
	}
	u.start()
	if !strings.Contains(u.message, "executor prompt") {
		t.Errorf("message = %q", u.message)
	}

	// Resetting the prompts fixes them; valid settings are saved.
	def := session.DefaultAgent(backend.Executor)
	f.system.SetText(def.System)
	f.prompt.SetText(def.Prompt)
	frame(t, u)
	if u.invalid != nil {
		t.Fatal(u.invalid)
	}
	u.saveSettings()
	saved, err := session.Load(folder)
	if err != nil || saved.Agents[backend.Executor].Backend != "codex" || saved.Rounds != 3 {
		t.Errorf("saved %+v, %v", saved, err)
	}
}

func TestStartNeedsLogin(t *testing.T) {
	for _, k := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN"} {
		t.Setenv(k, "")
	}
	u := newUI(nil, repo(t))
	u.runtime.Value = "podman"
	frame(t, u)
	u.start()
	if !strings.Contains(u.message, "no login") {
		t.Errorf("message = %q", u.message)
	}
	frame(t, u)
	screenshot(t, u, "login")
}

func TestStartNeedsRepo(t *testing.T) {
	folder := t.TempDir()
	u := newUI(nil, folder)
	if s, bad := u.statusText(); !bad || s != notRepo {
		t.Errorf("status = %q", s)
	}
	u.start()
	if u.message != notRepo || u.launchedPID != 0 {
		t.Errorf("message = %q, launched = %d", u.message, u.launchedPID)
	}
	frame(t, u)
	screenshot(t, u, "norepo")

	// Running git init in the folder is noticed.
	if err := git.Init(folder); err != nil {
		t.Fatal(err)
	}
	u.poll()
	if s, bad := u.statusText(); bad || !u.isRepo {
		t.Errorf("status = %q, repo = %v", s, u.isRepo)
	}

	// So is pressing Run git init.
	folder = t.TempDir()
	u.openFolder(folder)
	frame(t, u)
	u.initBtn.Click()
	frame(t, u)
	if s, bad := u.statusText(); bad || !u.isRepo || !git.IsRepo(folder) {
		t.Errorf("status = %q, repo = %v, %v", s, u.isRepo, git.IsRepo(folder))
	}
}

func TestOpenFolder(t *testing.T) {
	freshPrefs(t)
	u := newUI(nil, "")
	file := filepath.Join(t.TempDir(), "file")
	write(t, file, "")
	u.openFolder(file)
	if u.folder != "" || !strings.Contains(u.message, "not a folder") {
		t.Errorf("folder = %q, message = %q", u.folder, u.message)
	}
	folder := t.TempDir()
	write(t, filepath.Join(folder, session.LocalDir, "config.json"), "{")
	u.openFolder(folder)
	if u.folder != folder || !strings.Contains(u.message, "defaults") {
		t.Errorf("folder = %q, message = %q", u.folder, u.message)
	}
}

func TestStatusText(t *testing.T) {
	u := newUI(nil, repo(t))
	if s, _ := u.statusText(); s != "Not started yet." {
		t.Errorf("status = %q", s)
	}
	u.launchedPID = os.Getpid()
	if s, _ := u.statusText(); s != "Starting…" {
		t.Errorf("status = %q", s)
	}
	u.launchedPID = 1 << 30
	if _, bad := u.statusText(); !bad {
		t.Error("an engine that exited is not reported")
	}
	u.launchedPID, u.hasStatus = 0, true
	for state, want := range map[session.State]string{
		session.Stopped:  "Stopped.",
		session.Finished: "Ran the rounds asked for, up to round 0.",
		session.Running:  "The session stopped unexpectedly; see the console.",
	} {
		u.status = session.Status{PID: 1 << 30, State: state}
		if s, _ := u.statusText(); s != want {
			t.Errorf("%s: status = %q, want %q", state, s, want)
		}
	}
	u.status = session.Status{PID: os.Getpid(), State: session.Starting}
	if s, _ := u.statusText(); !strings.Contains(s, "container") {
		t.Errorf("status = %q", s)
	}
}

func TestFirstLines(t *testing.T) {
	if s, more := firstLines("a\nb\nc", 3); s != "a\nb\nc" || more != 0 {
		t.Errorf("firstLines = %q, %d", s, more)
	}
	if s, more := firstLines("a\nb\nc\nd", 2); s != "a\nb" || more != 2 {
		t.Errorf("firstLines = %q, %d", s, more)
	}
}
