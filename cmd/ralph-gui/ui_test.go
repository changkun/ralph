package main

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
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

func TestNoFolder(t *testing.T) {
	freshPrefs(t)
	u := newUI(nil, "")
	frame(t, u)
	if s, _ := u.statusText(); !strings.Contains(s, "Choose a project folder") {
		t.Errorf("status = %q", s)
	}
	u.start()
	if u.message == "" {
		t.Error("Start without a folder did nothing to say so")
	}
	screenshot(t, u, "empty")
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
	if k := kinds(u.entries); k != "C" || !strings.Contains(u.entries[0].text, "Ralph loop starting") {
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
	// Lists learn their scroll range in a frame for the events after it.
	now := time.Now()
	for range 3 {
		frameAt(t, u, now)
	}
	l := &u.entryList
	if !u.following || l.Position.BeforeEnd || l.Position.Offset == 0 {
		t.Fatalf("following = %v, position = %+v; want the end", u.following, l.Position)
	}
	screenshot(t, u, "follow")

	// The view stays at the end as the session prints more.
	offset := l.Position.Offset
	log.WriteString("more\nand more\n")
	write(t, filepath.Join(folder, session.LocalDir, "engine.log"), log.String())
	u.poll()
	frameAt(t, u, now)
	if !u.following || l.Position.BeforeEnd || l.Position.Offset <= offset {
		t.Errorf("following = %v, position = %+v; want the new end, past %d", u.following, l.Position, offset)
	}

	// Scrolling up stops following.
	scroll(u, 1000, 500, -300)
	frameAt(t, u, now)
	if u.following || !l.Position.BeforeEnd {
		t.Errorf("following = %v, position = %+v; want to stop following", u.following, l.Position)
	}

	// Scrolling back to the end follows again, and scrolling past it
	// stretches the list, which then springs back.
	scroll(u, 1000, 500, 1000)
	frameAt(t, u, now)
	if !u.following || u.entryElastic.offset != 0 {
		t.Errorf("following = %v, position = %+v, elastic offset = %v; want to follow again", u.following, l.Position, u.entryElastic.offset)
	}
	frameAt(t, u, now)
	scroll(u, 1000, 500, 100)
	frameAt(t, u, now)
	if u.entryElastic.offset >= 0 {
		t.Errorf("elastic offset = %v; want the list pulled up", u.entryElastic.offset)
	}
	for i := range 40 {
		frameAt(t, u, now.Add(time.Second+time.Duration(i)*16*time.Millisecond))
	}
	if u.entryElastic.offset != 0 {
		t.Errorf("elastic offset = %v; want it back at 0", u.entryElastic.offset)
	}

	// Short content starts at the top.
	write(t, filepath.Join(folder, session.LocalDir, "engine.log"), "one line\n")
	u.showView(viewConsole)
	u.poll()
	for range 2 {
		frame(t, u)
	}
	if l.ScrollToEnd || l.Position.Offset != 0 {
		t.Errorf("scroll to end = %v, position = %+v; want the top", l.ScrollToEnd, l.Position)
	}

	// Once it no longer fits, the view moves to its end.
	write(t, filepath.Join(folder, session.LocalDir, "engine.log"), "one line\n"+log.String())
	u.poll()
	for range 2 {
		frame(t, u)
	}
	if !u.following || l.Position.BeforeEnd || l.Position.Offset == 0 {
		t.Errorf("following = %v, position = %+v; want the end", u.following, l.Position)
	}
	write(t, filepath.Join(folder, session.LocalDir, "engine.log"), "one line\n")
	u.poll()
	for range 3 {
		frame(t, u)
	}
	scroll(u, 1000, 500, -100)
	frame(t, u)
	if u.entryElastic.offset <= 0 {
		t.Errorf("elastic offset = %v; want the list pulled down", u.entryElastic.offset)
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
