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
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"gioui.org/x/explorer"

	"github.com/changkun/ralph/internal/backend"
	"github.com/changkun/ralph/internal/git"
	"github.com/changkun/ralph/internal/loop"
	"github.com/changkun/ralph/internal/session"
	"github.com/changkun/ralph/internal/transcript"
)

// pollInterval is how often the app rereads the session's files.
const pollInterval = 500 * time.Millisecond

// liveRounds is how many of the latest rounds the live view shows.
const liveRounds = 10

// The views of the transcript pane. View firstTurnView+i shows turns[i].
const (
	viewLive      = 0 // the latest rounds, following the session
	viewConsole   = 1 // the engine log
	firstTurnView = 2
)

// UI is the app's state.
type UI struct {
	w     *app.Window // nil in tests
	th    *material.Theme
	expl  *explorer.Explorer
	prefs prefs

	folder      string
	isRepo      bool             // whether the folder is in a git repository
	settings    session.Settings // as the form shows them
	saved       session.Settings // as last loaded or saved
	invalid     error            // why settings cannot be used, if they cannot
	logins      []session.Login
	status      session.Status
	hasStatus   bool
	launchedPID int    // the engine this app started, until it reports in
	finished    int    // rounds that finished, which a start continues after
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

	// The columns' layout.
	splitters     [2]splitter
	sidebarScroll scroller
	turnsScroll   scroller
	entryScroll   scroller

	// The transcript pane.
	turns      []session.TurnLog
	turnBtns   []widget.Clickable // one per view
	turnList   widget.List
	view       int
	following  bool // whether the view stays at its end as it grows
	entries    []entry
	entryList  widget.List
	expandBtns []widget.Clickable // one per entry
	expanded   map[string]bool    // events whose whole output is shown
	latestBtn  widget.Clickable
	markdownOn widget.Bool
	cache      map[string]turnCache // events of each turn's transcript
	mdCache    map[string][]mdBlock // parsed Markdown by source
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

type turnCache struct {
	key    string // the transcript file's path, size, and time
	events []transcript.Event
}

type entryKind uint8

const (
	entryTurn    entryKind = iota // a turn's heading
	entryEvent                    // one event of a turn
	entryNote                     // a note about the session, such as why it failed
	entryConsole                  // lines of the engine log
)

// entry is one item of the transcript pane.
type entry struct {
	kind  entryKind
	turn  session.TurnLog
	event transcript.Event
	text  string
	bad   bool
	first bool // for a console line, whether it is the first or last
	last  bool
	key   string // identifies an event, to remember whether it is expanded
}

func newUI(w *app.Window, folder string) *UI {
	th := material.NewTheme()
	th.Shaper = newShaper()
	th.Face = sans
	th.TextSize = 15
	th.Palette = material.Palette{Fg: ink, Bg: white, ContrastBg: accent, ContrastFg: white}
	u := &UI{
		w:        w,
		th:       th,
		prefs:    loadPrefs(),
		chosen:   make(chan chosenFolder, 1),
		roles:    map[backend.Role]*roleForm{},
		expanded: map[string]bool{},
		cache:    map[string]turnCache{},
		mdCache:  map[string][]mdBlock{},
	}
	if w != nil {
		u.expl = explorer.NewExplorer(w)
	}
	u.folderEd.SingleLine, u.folderEd.Submit = true, true
	u.rounds.SingleLine, u.rounds.Filter = true, "0123456789"
	u.sidebar.Axis = layout.Vertical
	u.turnList.Axis = layout.Vertical
	u.entryList.Axis = layout.Vertical
	u.markdownOn.Value = !u.prefs.PlainText
	for _, role := range session.Roles {
		f := &roleForm{}
		f.model.SingleLine = true
		u.roles[role] = f
	}
	u.setSettings(session.Defaults())
	if fi, err := os.Stat(u.prefs.Folder); folder == "" && err == nil && fi.IsDir() {
		folder = u.prefs.Folder // reopen the last folder, if it is still there
	}
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
			u.prefs.save()
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
	if s.Rounds > 0 {
		u.rounds.SetText(strconv.Itoa(s.Rounds))
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
	s.Rounds, _ = strconv.Atoi(u.rounds.Text())
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
	return a.Pattern == b.Pattern && a.Runtime == b.Runtime && a.Rounds == b.Rounds && maps.Equal(a.Agents, b.Agents)
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
	u.isRepo = false
	u.showView(viewLive)
	u.entries = nil
	clear(u.cache)
	clear(u.expanded)
	u.prefs.Folder = abs
	u.prefs.save()
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

// starting reports whether the session is getting ready to run its first
// turn.
func (u *UI) starting() bool {
	if u.launchedPID != 0 {
		return session.Alive(u.launchedPID)
	}
	return u.hasStatus && u.status.State == session.Starting && u.status.Active()
}

func (u *UI) start() {
	u.message = ""
	if u.folder == "" {
		u.message = "Choose a project folder first."
		return
	}
	// Ralph commits the folder after each round, but it does not make it
	// a repository unasked.
	if u.isRepo = git.IsRepo(u.folder); !u.isRepo {
		u.message = notRepo
		return
	}
	if u.invalid != nil {
		u.message = u.invalid.Error()
		return
	}
	for _, l := range u.logins {
		if len(l.Found) == 0 {
			u.message = fmt.Sprintf("%s has no login to use in a container: %s.", backendTitle(l.Backend), l.Help)
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
	u.showView(viewLive)
	u.poll()
}

func (u *UI) stop() {
	u.message = ""
	if err := session.Stop(u.folder); err != nil {
		u.message = err.Error()
	}
	u.poll()
}

// showView switches the transcript pane to view v, from its start, or
// from its end if it follows the session.
func (u *UI) showView(v int) {
	u.view, u.following = v, true
	u.entryList.Position = layout.Position{}
}

// poll rereads the session's status and logs.
func (u *UI) poll() {
	u.lastPoll = time.Now()
	if u.folder == "" {
		return
	}
	if !u.isRepo {
		u.isRepo = git.IsRepo(u.folder) // until the user runs git init
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
	u.finished = loop.ResumeRound(filepath.Join(u.folder, session.LogDir), u.settings.Pattern)
	for len(u.turnBtns) < len(u.turns)+firstTurnView {
		u.turnBtns = append(u.turnBtns, widget.Clickable{})
	}
	if u.view >= len(u.turns)+firstTurnView {
		u.showView(viewLive)
	}
	switch u.view {
	case viewLive:
		u.entries = u.liveEntries()
	case viewConsole:
		u.entries = u.consoleEntries(0)
	default:
		u.entries = u.turnEntries(u.turns[u.view-firstTurnView])
	}
	for len(u.expandBtns) < len(u.entries) {
		u.expandBtns = append(u.expandBtns, widget.Clickable{})
	}
}

// liveEntries returns the turns of the latest rounds, followed by what the
// session is doing if it has no turn in progress.
func (u *UI) liveEntries() []entry {
	var es []entry
	if n := len(u.turns); n > 0 {
		last := u.turns[n-1].Round
		for _, t := range u.turns {
			if t.Round > last-liveRounds {
				es = append(es, u.turnEntries(t)...)
			}
		}
	}
	if u.starting() {
		es = append(es, entry{kind: entryNote, text: "Preparing the session. The first run builds the agent image, which takes a few minutes."})
		es = append(es, u.consoleEntries(30)...)
	} else if text, bad := u.statusText(); bad {
		es = append(es, entry{kind: entryNote, text: text, bad: true})
	}
	return es
}

// consoleEntries returns the engine log, or its last n lines if n > 0.
func (u *UI) consoleEntries(n int) []entry {
	log := strings.TrimRight(session.EngineLog(u.folder, 256<<10), "\n")
	if log == "" {
		return nil
	}
	lines := strings.Split(log, "\n")
	if n > 0 && len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	es := make([]entry, len(lines))
	for i, line := range lines {
		if line == "" {
			line = " " // an empty label would have no height
		}
		es[i] = entry{kind: entryConsole, text: line, first: i == 0, last: i == len(lines)-1}
	}
	return es
}

// turnEntries returns a turn's heading and events.
func (u *UI) turnEntries(t session.TurnLog) []entry {
	es := []entry{{kind: entryTurn, turn: t}}
	for i, e := range u.events(t.Path) {
		es = append(es, entry{kind: entryEvent, turn: t, event: e, key: fmt.Sprintf("%s#%d", t.Path, i)})
	}
	return es
}

// events returns the events in a transcript, rereading it only if it
// changed.
func (u *UI) events(path string) []transcript.Event {
	fi, err := os.Stat(path)
	if err != nil {
		return nil
	}
	key := fmt.Sprintf("%d %d", fi.Size(), fi.ModTime().UnixNano())
	if c, ok := u.cache[path]; ok && c.key == key {
		return c.events
	}
	events, err := transcript.ReadFile(path)
	if err != nil {
		return u.cache[path].events
	}
	u.cache[path] = turnCache{key: key, events: events}
	return events
}

// markdown returns src parsed, from the cache if it was parsed before.
func (u *UI) markdown(src string) []mdBlock {
	if b, ok := u.mdCache[src]; ok {
		return b
	}
	if len(u.mdCache) > 4000 {
		clear(u.mdCache)
	}
	b := parseMarkdown(src)
	u.mdCache[src] = b
	return b
}

// turnState says how a turn went: done, in progress, or not finished.
func (u *UI) turnState(t session.TurnLog) (mark string, ok, running bool) {
	switch {
	case t.Done:
		return "✓", true, false
	case len(u.turns) > 0 && t == u.turns[len(u.turns)-1] && u.active():
		return "…", true, true
	}
	return "✗", false, false
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
			u.showView(i)
			u.poll()
		}
	}
	for i := range u.expandBtns {
		if u.expandBtns[i].Clicked(gtx) && i < len(u.entries) {
			k := u.entries[i].key
			u.expanded[k] = !u.expanded[k]
		}
	}
	if u.latestBtn.Clicked(gtx) {
		u.following = true
	}
	if u.markdownOn.Update(gtx) {
		u.prefs.PlainText = !u.markdownOn.Value
		u.prefs.save()
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

// statusText describes the session's state, and whether it is a problem.
func (u *UI) statusText() (string, bool) {
	if u.folder == "" {
		return "Choose a project folder to begin.", false
	}
	if !u.isRepo {
		return notRepo, true
	}
	if u.launchedPID != 0 {
		if session.Alive(u.launchedPID) {
			return "Starting…", false
		}
		return "The session exited before it started; see the console.", true
	}
	if !u.hasStatus {
		return "Not started yet.", false
	}
	st := u.status
	if st.Active() {
		if st.State == session.Starting {
			return "Preparing the folder and the container…", false
		}
		if st.Role == "" {
			return "Running.", false
		}
		if st.Until > 0 {
			return fmt.Sprintf("Running round %d of %d: %s.", st.Round, st.Until, roleTitle(st.Role)), false
		}
		return fmt.Sprintf("Running round %d: %s.", st.Round, roleTitle(st.Role)), false
	}
	switch st.State {
	case session.Starting, session.Running:
		return "The session stopped unexpectedly; see the console.", true
	case session.Stopped:
		return "Stopped.", false
	case session.Finished:
		return fmt.Sprintf("Ran the rounds asked for, up to round %d.", st.Round), false
	case session.Failed:
		return "Failed: " + st.Error, true
	}
	return "", false
}

const notRepo = "This folder is not a git repository. Run git init in it first."

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

func backendTitle(name string) string {
	switch name {
	case "claude":
		return "Claude Code"
	case "codex":
		return "Codex"
	}
	return name
}
