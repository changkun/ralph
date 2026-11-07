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

	"gioui.org/gpu/headless"
	"gioui.org/io/input"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/changkun/ralph/internal/backend"
	"github.com/changkun/ralph/internal/loop"
	"github.com/changkun/ralph/internal/session"
)

const width, height = 1360, 860

// frame lays out one frame of u and returns its operations.
func frame(t *testing.T, u *UI) *op.Ops {
	t.Helper()
	ops := new(op.Ops)
	gtx := layout.Context{
		Source:      u.testRouter().Source(),
		Ops:         ops,
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
		Constraints: layout.Exact(image.Pt(width, height)),
		Now:         time.Now(),
	}
	u.layout(gtx)
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

// project returns a folder with the logs of a session that finished two
// turns and failed in a third.
func project(t *testing.T) string {
	t.Helper()
	folder := t.TempDir()
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

func TestNoFolder(t *testing.T) {
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

func TestFollowsSession(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "x")
	folder := project(t)
	u := newUI(nil, folder)
	if len(u.turns) != 3 {
		t.Fatalf("turns = %+v", u.turns)
	}
	// The session failed, so the engine log, which has the error, is shown.
	if u.view != 0 || len(u.logLines) != 2 {
		t.Errorf("view = %d, log = %q; want the engine log", u.view, u.logLines)
	}
	if s, bad := u.statusText(); !bad || !strings.Contains(s, "not supported") {
		t.Errorf("status = %q", s)
	}
	frame(t, u)
	screenshot(t, u, "log")

	// Choosing a turn shows its transcript and stops following.
	u.view, u.follow.Value = 2, false
	u.poll()
	if u.follow.Value || len(u.events) != 5 {
		t.Errorf("follow = %v, events = %+v", u.follow.Value, u.events)
	}
	for _, f := range u.roles {
		f.edit.Value = true
	}
	frame(t, u)
	screenshot(t, u, "turn")

	// Following a running session shows its latest turn.
	write(t, filepath.Join(folder, session.LocalDir, "status.json"),
		`{"pid":`+strconv.Itoa(os.Getpid())+`,"state":"running","round":2,"role":"strategist"}`)
	u.follow.Value = true
	u.poll()
	if u.view != 3 || !u.active() {
		t.Errorf("view = %d, active = %v; want the running turn", u.view, u.active())
	}
	if s, _ := u.statusText(); s != "Running round 2: Strategist." {
		t.Errorf("status = %q", s)
	}
	frame(t, u)
	screenshot(t, u, "running")

	// Another folder can be opened while the session runs.
	other := t.TempDir()
	u.openFolder(other)
	if u.folder != other || u.active() {
		t.Errorf("folder = %q, active = %v; want the other, idle folder", u.folder, u.active())
	}
	u.openFolder(folder)
	if !u.active() || u.view != 3 {
		t.Errorf("active = %v, view = %d; want to follow the running session again", u.active(), u.view)
	}
}

func TestSettings(t *testing.T) {
	folder := t.TempDir()
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
	if u.settings.Pattern != loop.ThinkActEvaluatorArchivist || u.settings.MaxRounds != 3 || u.settings.Agents[backend.Executor].Model != "gpt-5" {
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
	if err != nil || saved.Agents[backend.Executor].Backend != "codex" || saved.MaxRounds != 3 {
		t.Errorf("saved %+v, %v", saved, err)
	}
}

func TestStartNeedsLogin(t *testing.T) {
	for _, k := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN"} {
		t.Setenv(k, "")
	}
	u := newUI(nil, t.TempDir())
	u.runtime.Value = "podman"
	frame(t, u)
	u.start()
	if !strings.Contains(u.message, "no login") {
		t.Errorf("message = %q", u.message)
	}
	frame(t, u)
	screenshot(t, u, "login")
}

func TestOpenFolder(t *testing.T) {
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
	u := newUI(nil, t.TempDir())
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
		session.Finished: "Finished its last round.",
		session.Running:  "The session stopped unexpectedly; see the engine log.",
	} {
		u.status = session.Status{PID: 1 << 30, State: state}
		if s, _ := u.statusText(); s != want {
			t.Errorf("%s: status = %q, want %q", state, s, want)
		}
	}
	u.status = session.Status{PID: os.Getpid(), State: session.Starting}
	if s, _ := u.statusText(); !strings.Contains(s, "agent image") {
		t.Errorf("status = %q", s)
	}
}

func TestClipLines(t *testing.T) {
	if got := clipLines("a\nb\nc", 3); got != "a\nb\nc" {
		t.Errorf("clipLines = %q", got)
	}
	if got := clipLines("a\nb\nc\nd", 2); got != "a\nb\n… 2 more lines" {
		t.Errorf("clipLines = %q", got)
	}
}
