package session

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/changkun/ralph/internal/backend"
	"github.com/changkun/ralph/internal/loop"
)

// isolate gives the test an empty home directory, no credentials, and a
// git identity, so that it does not depend on this machine's setup.
func isolate(t *testing.T) (home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME"} {
		t.Setenv(k, "T")
	}
	for _, k := range []string{"GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL"} {
		t.Setenv(k, "t@t")
	}
	for _, l := range logins {
		for _, k := range l.env {
			t.Setenv(k, "")
		}
	}
	return home
}

// fakeBin installs shell scripts at the front of PATH and returns their
// directory.
func fakeBin(t *testing.T, scripts map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range scripts {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return dir
}

// fakeClaude answers every turn with "did it" and logs its arguments.
const fakeClaude = `printf '%s\n' "$@" >> "$(dirname "$0")/claude.log"
echo '{"type":"result","result":"did it"}'`

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDefaults(t *testing.T) {
	s := Defaults()
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, role := range Roles {
		if a := s.Agents[role]; a.Backend != "claude" || a.Prompt == "" {
			t.Errorf("%s agent = %+v", role, a)
		}
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Settings)
		want   string
	}{
		{"pattern", func(s *Settings) { s.Pattern = "bad" }, "unknown pattern"},
		{"runtime", func(s *Settings) { s.Runtime = "lxc" }, "unknown runtime"},
		{"rounds", func(s *Settings) { s.MaxRounds = -1 }, "negative"},
		{"backend", func(s *Settings) { s.Agents[backend.Executor] = Agent{Backend: "gpt"} }, `executor: unknown backend "gpt"`},
		{"template", func(s *Settings) {
			a := s.Agents[backend.Strategist]
			a.Prompt = "{{.Nope}}"
			s.Agents[backend.Strategist] = a
		}, "strategist prompt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Defaults()
			tt.change(&s)
			if err := s.Validate(); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("err = %v, want it to include %q", err, tt.want)
			}
		})
	}
	// Roles outside the pattern are not checked.
	s := Defaults()
	s.Agents[backend.Archivist] = Agent{}
	if err := s.Validate(); err != nil {
		t.Errorf("unused role checked: %v", err)
	}
}

func TestSaveLoad(t *testing.T) {
	folder := t.TempDir()
	s := Defaults()
	s.Pattern = loop.ThinkActEvaluator
	s.Runtime = Host
	s.MaxRounds = 3
	exec := s.Agents[backend.Executor]
	exec.Backend, exec.Model, exec.System = "codex", "gpt-5", "be brief"
	s.Agents[backend.Executor] = exec
	if err := s.Save(folder); err != nil {
		t.Fatal(err)
	}
	saved := read(t, filepath.Join(folder, LocalDir, "config.json"))
	if strings.Count(saved, `"system"`) != 1 || strings.Contains(saved, `"prompt"`) {
		t.Errorf("built-in prompts were saved:\n%s", saved)
	}
	got, err := Load(folder)
	if err != nil {
		t.Fatal(err)
	}
	if got.Pattern != s.Pattern || got.Runtime != s.Runtime || got.MaxRounds != 3 {
		t.Errorf("loaded %+v", got)
	}
	for _, role := range Roles {
		if got.Agents[role] != s.Agents[role] {
			t.Errorf("%s: loaded %+v, want %+v", role, got.Agents[role], s.Agents[role])
		}
	}
}

func TestLoadDefaults(t *testing.T) {
	folder := t.TempDir()
	s, err := Load(folder)
	if err != nil || s.Pattern != loop.ThinkAct || len(s.Agents) != len(Roles) {
		t.Errorf("Load of a new folder = %+v, %v", s, err)
	}
	// Unknown roles and missing fields are ignored.
	path := filepath.Join(folder, LocalDir, "config.json")
	if err := writeFile(path, []byte(`{"agents":{"oracle":{"backend":"codex"},"executor":{"model":"m"}}}`)); err != nil {
		t.Fatal(err)
	}
	s, err = Load(folder)
	if err != nil {
		t.Fatal(err)
	}
	if a := s.Agents[backend.Executor]; a.Backend != "claude" || a.Model != "m" || a.Prompt != DefaultAgent(backend.Executor).Prompt {
		t.Errorf("executor = %+v", a)
	}
	if _, ok := s.Agents["oracle"]; ok {
		t.Error("an unknown role was loaded")
	}

	if err := writeFile(path, []byte(`{`)); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(folder); err == nil {
		t.Error("Load accepted malformed JSON")
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(folder); err == nil && os.Getuid() != 0 {
		t.Error("Load ignored an unreadable file")
	}
}

func TestLogins(t *testing.T) {
	home := isolate(t)
	s := Defaults()
	s.Agents[backend.Executor] = Agent{Backend: "codex"}

	ls := s.Logins()
	if len(ls) != 2 || ls[0].Backend != "claude" || ls[1].Backend != "codex" || len(ls[0].Found)+len(ls[1].Found) != 0 {
		t.Fatalf("Logins = %+v", ls)
	}
	if _, err := s.sandboxOptions("/p", nil); err == nil || !strings.Contains(err.Error(), "claude setup-token") {
		t.Errorf("err = %v, want help for claude", err)
	}

	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "x")
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "auth.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	ls = s.Logins()
	if !slices.Equal(ls[0].Found, []string{"CLAUDE_CODE_OAUTH_TOKEN"}) || !slices.Equal(ls[1].Found, []string{"~/.codex/auth.json"}) {
		t.Errorf("Logins = %+v", ls)
	}
	o, err := s.sandboxOptions("/p", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(o.Env) != 5 || o.Files[".codex/auth.json"] != filepath.Join(home, ".codex", "auth.json") || !slices.Equal(o.Hidden, []string{LocalDir}) {
		t.Errorf("sandbox options = %+v", o)
	}

	// A backend that no role of the pattern uses needs no login.
	s.Agents[backend.Executor] = Agent{Backend: "claude"}
	s.Agents[backend.Archivist] = Agent{Backend: "codex"}
	if ls := s.Logins(); len(ls) != 1 {
		t.Errorf("Logins = %+v, want only claude", ls)
	}
}

func hostSettings() Settings {
	s := Defaults()
	s.Runtime = Host
	s.MaxRounds = 1
	return s
}

func TestRunOnHost(t *testing.T) {
	isolate(t)
	bin := fakeBin(t, map[string]string{"claude": `[ "$1" = -p ] && echo hi > "$(pwd)/out.txt"
` + fakeClaude})
	folder := t.TempDir()
	var out strings.Builder
	if err := Run(context.Background(), folder, hostSettings(), &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Runtime: host", "strategist: claude (default model)", "committed locally"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output does not contain %q:\n%s", want, out.String())
		}
	}
	if !strings.Contains(read(t, filepath.Join(bin, "claude.log")), folder) {
		t.Error("prompts do not name the folder")
	}
	st, err := ReadStatus(folder)
	if err != nil || st.State != Finished || st.Round != 1 || st.Role != "" || st.PID != os.Getpid() {
		t.Errorf("status = %+v, %v", st, err)
	}
	if st.Active() {
		t.Error("a finished session is active")
	}
	// The session's files are neither committed nor left as changes.
	files, err := exec.Command("git", "-C", folder, "ls-files").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(files), "local/") || !strings.Contains(string(files), "out.txt") {
		t.Errorf("committed files:\n%s", files)
	}
	if status, _ := exec.Command("git", "-C", folder, "status", "--porcelain").Output(); len(status) > 0 {
		t.Errorf("uncommitted changes:\n%s", status)
	}
	turns := Turns(folder)
	if len(turns) != 2 || turns[0].Role != backend.Strategist || turns[1].Role != backend.Executor || !turns[1].Done {
		t.Errorf("turns = %+v", turns)
	}
}

func TestRunInSandbox(t *testing.T) {
	isolate(t)
	t.Setenv("ANTHROPIC_API_KEY", "secret")
	// The fake podman runs exec'd commands on the host, in the project.
	folder := t.TempDir()
	bin := fakeBin(t, map[string]string{
		"claude": fakeClaude,
		"podman": `printf '%s\n' "$@" >> "$(dirname "$0")/podman.log"
case "$1" in
exec) shift 4; cd '` + folder + `' && exec "$@" ;;
run) echo id ;;
esac`,
	})
	s := Defaults()
	s.Runtime = "podman"
	s.MaxRounds = 1
	if err := Run(context.Background(), folder, s, &strings.Builder{}); err != nil {
		t.Fatal(err)
	}
	podman := read(t, filepath.Join(bin, "podman.log"))
	for _, want := range []string{"type=tmpfs,destination=/work/.ralph/local,notmpcopyup", "ANTHROPIC_API_KEY", "/work"} {
		if !strings.Contains(podman, want) {
			t.Errorf("podman was not passed %q:\n%s", want, podman)
		}
	}
	if strings.Contains(podman, "secret") {
		t.Error("the API key appears in podman's arguments")
	}
	if args := read(t, filepath.Join(bin, "claude.log")); !strings.Contains(args, "/work") || strings.Contains(args, folder) {
		t.Errorf("agents were not given the container's path:\n%s", args)
	}
	if !strings.HasSuffix(strings.TrimSpace(podman), "-ro") {
		t.Errorf("containers not removed at the end:\n%s", podman)
	}
}

func TestRunFailures(t *testing.T) {
	isolate(t)
	t.Run("invalid settings", func(t *testing.T) {
		s := hostSettings()
		s.Pattern = "bad"
		if err := Run(context.Background(), t.TempDir(), s, discard()); err == nil {
			t.Error("expected an error")
		}
	})
	t.Run("not a folder", func(t *testing.T) {
		if err := Run(context.Background(), "/dev/null", hostSettings(), discard()); err == nil {
			t.Error("expected an error")
		}
		if err := Run(context.Background(), filepath.Join(t.TempDir(), "missing"), hostSettings(), discard()); err == nil {
			t.Error("expected an error")
		}
	})
	t.Run("no login", func(t *testing.T) {
		folder := t.TempDir()
		s := hostSettings()
		s.Runtime = "docker"
		err := Run(context.Background(), folder, s, discard())
		if err == nil || !strings.Contains(err.Error(), "no login") {
			t.Errorf("err = %v", err)
		}
		if st, _ := ReadStatus(folder); st.State != Failed || st.Error != err.Error() {
			t.Errorf("status = %+v", st)
		}
	})
	t.Run("sandbox fails", func(t *testing.T) {
		t.Setenv("ANTHROPIC_API_KEY", "x")
		fakeBin(t, map[string]string{"podman": `echo "cannot connect" >&2; exit 125`})
		s := hostSettings()
		s.Runtime = "podman"
		if err := Run(context.Background(), t.TempDir(), s, discard()); err == nil || !strings.Contains(err.Error(), "agent image") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("agent fails", func(t *testing.T) {
		fakeBin(t, map[string]string{"claude": `echo 'Not logged in' >&2; exit 1`})
		folder := t.TempDir()
		if err := Run(context.Background(), folder, hostSettings(), discard()); err == nil || !strings.Contains(err.Error(), "Not logged in") {
			t.Errorf("err = %v", err)
		}
		turns := Turns(folder)
		if len(turns) != 1 || turns[0].Done {
			t.Errorf("turns = %+v, want one unfinished turn", turns)
		}
	})
}

func discard() *strings.Builder { return &strings.Builder{} }

func TestRunInterrupted(t *testing.T) {
	isolate(t)
	fakeBin(t, map[string]string{"claude": fakeClaude})
	folder := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Run(ctx, folder, hostSettings(), discard()); err != nil {
		t.Fatal(err)
	}
	if st, _ := ReadStatus(folder); st.State != Stopped {
		t.Errorf("status = %+v, want stopped", st)
	}
}

// startProcess starts a process that runs until it is interrupted, and
// records it as the active session on a new folder.
func startProcess(t *testing.T) (folder string, cmd *exec.Cmd) {
	t.Helper()
	cmd = exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	folder = t.TempDir()
	if err := writeFile(statusPath(folder), []byte(`{"pid":`+strconv.Itoa(cmd.Process.Pid)+`,"state":"running"}`)); err != nil {
		t.Fatal(err)
	}
	return folder, cmd
}

func TestRunRefusesSecondSession(t *testing.T) {
	isolate(t)
	folder, _ := startProcess(t)
	err := Run(context.Background(), folder, hostSettings(), discard())
	if err == nil || !strings.Contains(err.Error(), "already running") {
		t.Errorf("err = %v", err)
	}
}

func TestStop(t *testing.T) {
	folder, cmd := startProcess(t)
	st, err := ReadStatus(folder)
	if err != nil || !st.Active() {
		t.Fatalf("status = %+v, %v; want active", st, err)
	}
	if err := Stop(folder); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the process did not stop")
	}
	if err := Stop(folder); err == nil {
		t.Error("Stop succeeded with no active session")
	}
	if err := Stop(t.TempDir()); err == nil {
		t.Error("Stop succeeded on a folder without a session")
	}
}

func TestActive(t *testing.T) {
	for _, st := range []Status{
		{PID: os.Getpid(), State: Finished},
		{PID: 0, State: Running},
		{PID: 1 << 30, State: Running},
	} {
		if st.Active() {
			t.Errorf("%+v is active", st)
		}
	}
	if !(Status{PID: os.Getpid(), State: Starting}).Active() {
		t.Error("this process's session is not active")
	}
}

func TestReadStatusMissing(t *testing.T) {
	if _, err := ReadStatus(t.TempDir()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("err = %v, want ErrNotExist", err)
	}
}

func TestTurns(t *testing.T) {
	folder := t.TempDir()
	dir := filepath.Join(folder, LogDir)
	for _, name := range []string{
		"round-010-strategist.jsonl", "round-002-executor.jsonl", "round-002-executor.json",
		"round-002-strategist.jsonl", "round-002-strategist.json", "round-003-oracle.jsonl",
		"round-x-executor.jsonl", "notes.jsonl", "round-001-standalone.json",
	} {
		if err := writeFile(filepath.Join(dir, name), nil); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	for _, turn := range Turns(folder) {
		s := strconv.Itoa(turn.Round) + " " + string(turn.Role)
		if turn.Done {
			s += " done"
		}
		got = append(got, s)
	}
	if want := []string{"2 strategist done", "2 executor done", "10 strategist"}; !slices.Equal(got, want) {
		t.Errorf("turns = %q, want %q", got, want)
	}
	if Turns(t.TempDir()) != nil {
		t.Error("turns found in a folder without logs")
	}
}
