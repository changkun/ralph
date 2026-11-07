package main

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gioui.org/app"
	"gioui.org/font/gofont"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"gioui.org/x/explorer"

	"github.com/changkun/ralph/internal/backend"
	"github.com/changkun/ralph/internal/loop"
	"github.com/changkun/ralph/internal/session"
	"github.com/changkun/ralph/internal/transcript"
)

// pollInterval is how often the app rereads the session's files.
const pollInterval = 500 * time.Millisecond

// UI is the app's state.
type UI struct {
	w    *app.Window // nil in tests
	th   *material.Theme
	expl *explorer.Explorer

	folder      string
	settings    session.Settings // as the form shows them
	saved       session.Settings // as last loaded or saved
	invalid     error            // why settings cannot be used, if they cannot
	logins      []session.Login
	status      session.Status
	hasStatus   bool
	launchedPID int    // the engine this app started, until it reports in
	message     string // the outcome of the last action, if it failed
	lastPoll    time.Time

	chosen   chan chosenFolder
	choosing bool

	// The settings form.
	folderEd  widget.Editor
	chooseBtn widget.Clickable
	runtime   widget.Enum
	pattern   widget.Enum
	rounds    widget.Editor
	roles     map[backend.Role]*roleForm
	startBtn  widget.Clickable
	stopBtn   widget.Clickable
	sidebar   widget.List

	// The transcript view. View 0 is the engine log, and view i is turns[i-1].
	turns     []session.TurnLog
	turnBtns  []widget.Clickable
	turnList  widget.List
	follow    widget.Bool
	view      int
	shownView int // the view eventList's position belongs to
	events    []transcript.Event
	eventsKey string // identifies the file version events were read from
	logLines  []string
	eventList widget.List
}

type roleForm struct {
	backend widget.Enum
	model   widget.Editor
	system  widget.Editor
	prompt  widget.Editor
	edit    widget.Bool // whether the prompt editors are shown
	reset   widget.Clickable
}

type chosenFolder struct {
	path string
	err  error
}

func newUI(w *app.Window, folder string) *UI {
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()))
	th.TextSize = 14
	th.Palette.ContrastBg = accent
	u := &UI{
		w:      w,
		th:     th,
		chosen: make(chan chosenFolder, 1),
		roles:  map[backend.Role]*roleForm{},
	}
	if w != nil {
		u.expl = explorer.NewExplorer(w)
	}
	u.folderEd.SingleLine, u.folderEd.Submit = true, true
	u.rounds.SingleLine, u.rounds.Filter = true, "0123456789"
	u.sidebar.Axis = layout.Vertical
	u.turnList.Axis = layout.Vertical
	u.eventList.Axis = layout.Vertical
	u.follow.Value = true
	for _, role := range session.Roles {
		f := &roleForm{}
		f.model.SingleLine = true
		u.roles[role] = f
	}
	u.setSettings(session.Defaults())
	if folder != "" {
		u.openFolder(folder)
	}
	return u
}

// run handles the window's events until it is closed.
func (u *UI) run() error {
	go func() {
		for range time.Tick(pollInterval) {
			u.w.Invalidate()
		}
	}()
	var ops op.Ops
	for {
		e := u.w.Event()
		u.expl.ListenEvents(e)
		switch e := e.(type) {
		case app.DestroyEvent:
			u.saveSettings()
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			u.layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

// setSettings shows s in the form.
func (u *UI) setSettings(s session.Settings) {
	u.settings = s
	u.runtime.Value = s.Runtime
	u.pattern.Value = string(s.Pattern)
	if s.MaxRounds > 0 {
		u.rounds.SetText(strconv.Itoa(s.MaxRounds))
	} else {
		u.rounds.SetText("")
	}
	for role, f := range u.roles {
		a := s.Agents[role]
		f.backend.Value = a.Backend
		f.model.SetText(a.Model)
		f.system.SetText(a.System)
		f.prompt.SetText(a.Prompt)
	}
	u.check()
}

// readForm returns the settings the form shows.
func (u *UI) readForm() session.Settings {
	s := session.Settings{
		Pattern: loop.Pattern(u.pattern.Value),
		Runtime: u.runtime.Value,
		Agents:  map[backend.Role]session.Agent{},
	}
	s.MaxRounds, _ = strconv.Atoi(u.rounds.Text())
	for role, f := range u.roles {
		s.Agents[role] = session.Agent{
			Backend: f.backend.Value,
			Model:   strings.TrimSpace(f.model.Text()),
			System:  f.system.Text(),
			Prompt:  f.prompt.Text(),
		}
	}
	return s
}

func equal(a, b session.Settings) bool {
	return a.Pattern == b.Pattern && a.Runtime == b.Runtime && a.MaxRounds == b.MaxRounds && maps.Equal(a.Agents, b.Agents)
}

// check validates the settings and finds the logins they need.
func (u *UI) check() {
	u.invalid = u.settings.Validate()
	u.logins = nil
	if u.settings.Runtime != session.Host {
		u.logins = u.settings.Logins()
	}
}

// saveSettings saves changed, valid settings to the folder.
func (u *UI) saveSettings() {
	if u.folder == "" || u.invalid != nil || equal(u.settings, u.saved) {
		return
	}
	if err := u.settings.Save(u.folder); err != nil {
		u.message = "Could not save the settings: " + err.Error()
		return
	}
	u.saved = u.settings
}

// openFolder switches to the project folder dir.
func (u *UI) openFolder(dir string) {
	dir = strings.TrimSpace(dir)
	if strings.HasPrefix(dir, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, dir[2:])
		}
	}
	abs, err := filepath.Abs(dir)
	if err == nil {
		var fi os.FileInfo
		if fi, err = os.Stat(abs); err == nil && !fi.IsDir() {
			err = errors.New("not a folder")
		}
	}
	if err != nil {
		u.message = fmt.Sprintf("Cannot open %s: %v", dir, err)
		u.folderEd.SetText(u.folder)
		return
	}
	u.saveSettings()
	s, err := session.Load(abs)
	u.message = ""
	if err != nil {
		u.message = "Could not load the saved settings, so the defaults are shown: " + err.Error()
	}
	u.folder = abs
	u.folderEd.SetText(abs)
	u.setSettings(s)
	u.saved = s
	u.launchedPID = 0
	u.view, u.follow.Value = 0, true
	u.events, u.eventsKey, u.logLines = nil, "", nil
	u.poll()
	if u.w != nil {
		u.w.Option(app.Title("Ralph · " + filepath.Base(abs)))
	}
}

func (u *UI) chooseFolder() {
	if u.choosing || u.expl == nil {
		return
	}
	u.choosing = true
	go func() {
		path, err := u.expl.ChooseFolder()
		u.chosen <- chosenFolder{path, err}
		u.w.Invalidate()
	}()
}

// active reports whether a session is starting or running on the folder.
func (u *UI) active() bool {
	if u.launchedPID != 0 && session.Alive(u.launchedPID) {
		return true
	}
	return u.hasStatus && u.status.Active()
}

func (u *UI) start() {
	u.message = ""
	if u.folder == "" {
		u.message = "Choose a project folder first."
		return
	}
	if u.invalid != nil {
		u.message = u.invalid.Error()
		return
	}
	for _, l := range u.logins {
		if len(l.Found) == 0 {
			u.message = fmt.Sprintf("%s has no login to use in a container: %s.", l.Backend, l.Help)
			return
		}
	}
	// The engine reads the settings from the folder.
	if err := u.settings.Save(u.folder); err != nil {
		u.message = "Could not save the settings: " + err.Error()
		return
	}
	u.saved = u.settings
	exe, err := os.Executable()
	if err == nil {
		u.launchedPID, err = session.Launch(exe, u.folder)
	}
	if err != nil {
		u.message = "Could not start: " + err.Error()
		return
	}
	u.view, u.follow.Value = 0, true
	u.poll()
}

func (u *UI) stop() {
	u.message = ""
	if err := session.Stop(u.folder); err != nil {
		u.message = err.Error()
	}
	u.poll()
}

// poll rereads the session's status and logs.
func (u *UI) poll() {
	u.lastPoll = time.Now()
	if u.folder == "" {
		return
	}
	st, err := session.ReadStatus(u.folder)
	u.status, u.hasStatus = st, err == nil
	if u.hasStatus && st.PID == u.launchedPID {
		u.launchedPID = 0 // the engine has reported in
	}
	if u.settings.Runtime != session.Host {
		u.logins = u.settings.Logins()
	}
	u.turns = session.Turns(u.folder)
	for len(u.turnBtns) < len(u.turns)+1 {
		u.turnBtns = append(u.turnBtns, widget.Clickable{})
	}
	if u.follow.Value {
		u.view = u.followedView()
	}
	if u.view > len(u.turns) {
		u.view = 0
	}
	if u.view == 0 {
		u.logLines = strings.Split(strings.TrimRight(session.EngineLog(u.folder, 256<<10), "\n"), "\n")
		return
	}
	u.loadEvents(u.turns[u.view-1].Path)
}

// followedView returns the view to show when following the session: the
// engine log while it starts or after it failed, and the latest turn
// otherwise.
func (u *UI) followedView() int {
	switch {
	case u.launchedPID != 0, len(u.turns) == 0:
		return 0
	case u.hasStatus && u.status.State == session.Starting && u.status.Active():
		return 0
	case u.hasStatus && u.status.State == session.Failed:
		return 0
	}
	return len(u.turns)
}

func (u *UI) loadEvents(path string) {
	fi, err := os.Stat(path)
	if err != nil {
		u.events, u.eventsKey = nil, ""
		return
	}
	key := fmt.Sprintf("%s %d %d", path, fi.Size(), fi.ModTime().UnixNano())
	if key == u.eventsKey {
		return
	}
	events, err := transcript.ReadFile(path)
	if err != nil {
		return
	}
	u.events, u.eventsKey = events, key
}

// update handles input that changes the state, before the frame is laid
// out.
func (u *UI) update(gtx layout.Context) {
	select {
	case c := <-u.chosen:
		u.choosing = false
		switch {
		case c.err == nil && c.path != "":
			u.openFolder(c.path)
		case c.err != nil && !errors.Is(c.err, explorer.ErrUserDecline):
			u.message = "Could not choose a folder: " + c.err.Error()
		}
	default:
	}
	for {
		e, ok := u.folderEd.Update(gtx)
		if !ok {
			break
		}
		if _, ok := e.(widget.SubmitEvent); ok {
			u.openFolder(u.folderEd.Text())
		}
	}
	if u.chooseBtn.Clicked(gtx) {
		u.chooseFolder()
	}
	if u.startBtn.Clicked(gtx) {
		u.start()
	}
	if u.stopBtn.Clicked(gtx) {
		u.stop()
	}
	for role, f := range u.roles {
		if f.reset.Clicked(gtx) {
			def := session.DefaultAgent(role)
			f.system.SetText(def.System)
			f.prompt.SetText(def.Prompt)
		}
	}
	for i := range u.turnBtns {
		if u.turnBtns[i].Clicked(gtx) {
			u.view, u.follow.Value = i, false
			u.poll()
		}
	}
	if u.follow.Update(gtx) && u.follow.Value {
		u.poll()
	}
	if time.Since(u.lastPoll) >= pollInterval-50*time.Millisecond {
		u.poll()
	}
}

// sync takes in what the user changed in the form.
func (u *UI) sync(gtx layout.Context) {
	if s := u.readForm(); !equal(s, u.settings) {
		u.settings = s
		u.check()
		gtx.Execute(op.InvalidateCmd{})
	}
}

// statusText describes the session's state.
func (u *UI) statusText() (string, bool) {
	if u.folder == "" {
		return "Choose a project folder to begin.", false
	}
	if u.launchedPID != 0 {
		if session.Alive(u.launchedPID) {
			return "Starting…", false
		}
		return "The session exited before it started; see the engine log.", true
	}
	if !u.hasStatus {
		return "Not started yet.", false
	}
	st := u.status
	if st.Active() {
		if st.State == session.Starting {
			return "Preparing the folder and the container. The first run builds the agent image, which takes a few minutes.", false
		}
		if st.Role == "" {
			return "Running.", false
		}
		return fmt.Sprintf("Running round %d: %s.", st.Round, roleTitle(st.Role)), false
	}
	switch st.State {
	case session.Starting, session.Running:
		return "The session stopped unexpectedly; see the engine log.", true
	case session.Stopped:
		return "Stopped.", false
	case session.Finished:
		return "Finished its last round.", false
	case session.Failed:
		return "Failed: " + st.Error, true
	}
	return "", false
}

var roleInfo = map[backend.Role]string{
	backend.Standalone: "Plans and does the work alone.",
	backend.Strategist: "Decides the next objective. Read-only: it cannot change the project.",
	backend.Executor:   "Carries out the objective.",
	backend.Evaluator:  "Checks the executor's work.",
	backend.Archivist:  "Records what was learned in the memory file.",
}

func roleTitle(r backend.Role) string {
	s := string(r)
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
