// Package session runs Ralph on a project folder: it prepares the folder,
// starts the agents' sandbox, and runs the loop, recording the session's
// state in the folder for a GUI to follow.
//
// A project's session files live in LocalDir. Agents in a sandbox cannot see
// it, and Ralph does not commit it.
package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/changkun/ralph/internal/backend"
	"github.com/changkun/ralph/internal/git"
	"github.com/changkun/ralph/internal/loop"
	"github.com/changkun/ralph/internal/prompt"
	"github.com/changkun/ralph/internal/sandbox"
)

const (
	// LogDir holds the round logs, relative to the project folder.
	LogDir = ".ralph"
	// LocalDir holds the session files, relative to the project folder.
	LocalDir = ".ralph/local"
	// Host is the runtime that runs agents directly on this machine.
	Host = "host"
)

// Runtimes lists the runtimes a session can use: the container runtimes,
// then Host.
var Runtimes = append(slices.Clone(sandbox.Runtimes), Host)

// Roles lists every role, in the order they run in a round.
var Roles = []backend.Role{backend.Standalone, backend.Strategist, backend.Executor, backend.Evaluator, backend.Archivist}

// Agent configures the agent for one role.
type Agent struct {
	Backend string // "claude" or "codex"
	Model   string // empty for the CLI's default
	System  string // system prompt template
	Prompt  string // user prompt template
}

// DefaultAgent returns the built-in configuration for role.
func DefaultAgent(role backend.Role) Agent {
	t, _ := prompt.Default(string(role))
	return Agent{Backend: "claude", System: t.System, Prompt: t.Prompt}
}

// Settings configures a session.
type Settings struct {
	Pattern loop.Pattern
	Runtime string // one of Runtimes
	Rounds  int    // rounds to run each time the session starts; 0 means no limit
	Agents  map[backend.Role]Agent
}

// Defaults returns the default settings: the think+act pattern, the first
// installed container runtime, and Claude with the built-in prompts for
// every role.
func Defaults() Settings {
	s := Settings{Pattern: loop.ThinkAct, Runtime: sandbox.Detect(), Agents: map[backend.Role]Agent{}}
	if s.Runtime == "" {
		s.Runtime = sandbox.Runtimes[0]
	}
	for _, role := range Roles {
		s.Agents[role] = DefaultAgent(role)
	}
	return s
}

// Validate reports the first problem with s, if any.
func (s Settings) Validate() error {
	if !slices.Contains(loop.Patterns, s.Pattern) {
		return fmt.Errorf("unknown pattern %q", s.Pattern)
	}
	if !slices.Contains(Runtimes, s.Runtime) {
		return fmt.Errorf("unknown runtime %q; want one of %s", s.Runtime, strings.Join(Runtimes, ", "))
	}
	if s.Rounds < 0 {
		return errors.New("the number of rounds cannot be negative")
	}
	for _, role := range s.Pattern.Roles() {
		if _, err := s.Agents[role].template(role); err != nil {
			return err
		}
	}
	return nil
}

func (a Agent) template(role backend.Role) (*prompt.Template, error) {
	if backend.New(a.Backend, nil) == nil {
		return nil, fmt.Errorf("%s: unknown backend %q; want one of %s", role, a.Backend, strings.Join(backend.Names, ", "))
	}
	t, err := prompt.Parse(prompt.Text{System: a.System, Prompt: a.Prompt})
	if err != nil {
		return nil, fmt.Errorf("%s prompt: %w", role, err)
	}
	return t, nil
}

// file is how Settings are saved. Prompts equal to the built-in ones are
// left out, so that they follow changes to the built-in prompts.
type file struct {
	Pattern loop.Pattern               `json:"pattern"`
	Runtime string                     `json:"runtime"`
	Rounds  int                        `json:"rounds,omitempty"`
	Agents  map[backend.Role]fileAgent `json:"agents"`
}

type fileAgent struct {
	Backend string  `json:"backend"`
	Model   string  `json:"model,omitempty"`
	System  *string `json:"system,omitempty"`
	Prompt  *string `json:"prompt,omitempty"`
}

func configPath(folder string) string { return filepath.Join(folder, LocalDir, "config.json") }

// Load returns the settings saved in folder, with defaults for anything not
// saved.
func Load(folder string) (Settings, error) {
	s := Defaults()
	data, err := os.ReadFile(configPath(folder))
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	var f file
	if err := json.Unmarshal(data, &f); err != nil {
		return s, fmt.Errorf("%s: %w", configPath(folder), err)
	}
	if f.Pattern != "" {
		s.Pattern = f.Pattern
	}
	if f.Runtime != "" {
		s.Runtime = f.Runtime
	}
	s.Rounds = f.Rounds
	for role, fa := range f.Agents {
		a, ok := s.Agents[role]
		if !ok {
			continue
		}
		if fa.Backend != "" {
			a.Backend = fa.Backend
		}
		a.Model = fa.Model
		if fa.System != nil {
			a.System = *fa.System
		}
		if fa.Prompt != nil {
			a.Prompt = *fa.Prompt
		}
		s.Agents[role] = a
	}
	return s, nil
}

// Save saves s in folder.
func (s Settings) Save(folder string) error {
	f := file{Pattern: s.Pattern, Runtime: s.Runtime, Rounds: s.Rounds, Agents: map[backend.Role]fileAgent{}}
	for role, a := range s.Agents {
		def := DefaultAgent(role)
		fa := fileAgent{Backend: a.Backend, Model: a.Model}
		if a.System != def.System {
			fa.System = &a.System
		}
		if a.Prompt != def.Prompt {
			fa.Prompt = &a.Prompt
		}
		f.Agents[role] = fa
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return writeFile(configPath(folder), append(data, '\n'))
}

// writeFile replaces the file at path with data, creating its directory if
// needed. Readers never see a partly written file.
func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Login describes how a backend's agents can log in inside a sandbox.
type Login struct {
	Backend string
	Found   []string // the environment variables and files found
	Help    string   // what to provide when nothing is found
}

// logins lists what each backend can log in with in a sandbox: environment
// variables, and files in the home directory.
var logins = map[string]struct {
	env   []string
	files []string
	help  string
}{
	"claude": {
		env:  []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN"},
		help: "set ANTHROPIC_API_KEY, or CLAUDE_CODE_OAUTH_TOKEN from `claude setup-token`",
	},
	"codex": {
		env:   []string{"CODEX_API_KEY", "OPENAI_API_KEY"},
		files: []string{".codex/auth.json"},
		help:  "log in with `codex login`, or set CODEX_API_KEY",
	},
}

// backends returns the backends that the roles of s's pattern use.
func (s Settings) backends() []string {
	var names []string
	for _, role := range s.Pattern.Roles() {
		if name := s.Agents[role].Backend; !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

// Logins reports, for each backend s uses, what its agents can log in with
// inside a sandbox. Agents on the host use the CLIs' own logins instead.
func (s Settings) Logins() []Login {
	home, _ := os.UserHomeDir()
	var ls []Login
	for _, name := range s.backends() {
		l := Login{Backend: name, Help: logins[name].help}
		for _, env := range logins[name].env {
			if os.Getenv(env) != "" {
				l.Found = append(l.Found, env)
			}
		}
		for _, f := range logins[name].files {
			if _, err := os.Stat(filepath.Join(home, f)); err == nil {
				l.Found = append(l.Found, "~/"+f)
			}
		}
		ls = append(ls, l)
	}
	return ls
}

// sandboxOptions returns the options for a sandbox for s on folder, or an
// error if a backend has nothing to log in with.
func (s Settings) sandboxOptions(folder string, out io.Writer) (sandbox.Options, error) {
	o := sandbox.Options{
		Runtime: s.Runtime,
		Folder:  folder,
		Hidden:  []string{LocalDir},
		Files:   map[string]string{},
		Output:  out,
	}
	for _, l := range s.Logins() {
		if len(l.Found) == 0 {
			return o, fmt.Errorf("%s has no login to use in a container: %s", l.Backend, l.Help)
		}
		o.Env = append(o.Env, logins[l.Backend].env...)
		for _, f := range l.Found {
			if rel, ok := strings.CutPrefix(f, "~/"); ok {
				home, _ := os.UserHomeDir()
				o.Files[rel] = filepath.Join(home, rel)
			}
		}
	}
	return o, nil
}

// State is the state of a session.
type State string

// Session states.
const (
	Starting State = "starting" // preparing the folder or the sandbox
	Running  State = "running"  // running rounds
	Stopped  State = "stopped"  // interrupted
	Finished State = "finished" // ran the rounds it was asked to
	Failed   State = "failed"   // stopped by an error
)

// Status is the state of a project's latest session, which the session
// keeps up to date in LocalDir.
type Status struct {
	PID     int          `json:"pid"` // the process running the session
	State   State        `json:"state"`
	Pattern loop.Pattern `json:"pattern"`
	Runtime string       `json:"runtime"`
	Round   int          `json:"round,omitempty"` // the round in progress
	Until   int          `json:"until,omitempty"` // the last round to run, if there is a limit
	Role    backend.Role `json:"role,omitempty"`  // the role in progress
	Error   string       `json:"error,omitempty"` // why the session failed
	Time    time.Time    `json:"time"`            // when the status last changed
}

func statusPath(folder string) string { return filepath.Join(folder, LocalDir, "status.json") }

// ReadStatus returns the status of the latest session on folder. It returns
// an error wrapping os.ErrNotExist if there has been none.
func ReadStatus(folder string) (Status, error) {
	var st Status
	data, err := os.ReadFile(statusPath(folder))
	if err != nil {
		return st, err
	}
	err = json.Unmarshal(data, &st)
	return st, err
}

// Active reports whether the session is starting or running and its
// process is alive.
func (st Status) Active() bool {
	return (st.State == Starting || st.State == Running) && Alive(st.PID)
}

// Run runs a session on folder with s until ctx is cancelled, the last
// round finishes, or an agent fails; only the last case returns an error.
// It prints progress to out.
//
// Run makes folder a git repository if it is not one, and runs the agents
// in a sandbox unless s.Runtime is Host. It refuses to run while another
// session on folder is active.
func Run(ctx context.Context, folder string, s Settings, out io.Writer) (err error) {
	if err := s.Validate(); err != nil {
		return err
	}
	if folder, err = filepath.Abs(folder); err != nil {
		return err
	}
	if fi, err := os.Stat(folder); err != nil {
		return err
	} else if !fi.IsDir() {
		return fmt.Errorf("%s is not a folder", folder)
	}
	if st, err := ReadStatus(folder); err == nil && st.Active() && st.PID != os.Getpid() {
		return fmt.Errorf("Ralph is already running on %s, in process %d", folder, st.PID)
	}
	if err := os.MkdirAll(filepath.Join(folder, LocalDir), 0o755); err != nil {
		return err
	}

	st := Status{PID: os.Getpid(), State: Starting, Pattern: s.Pattern, Runtime: s.Runtime}
	update := func() {
		st.Time = time.Now()
		if data, err := json.Marshal(st); err == nil {
			writeFile(statusPath(folder), data)
		}
	}
	update()
	defer func() {
		st.Role = ""
		switch {
		case err != nil:
			st.State, st.Error = Failed, err.Error()
		case ctx.Err() != nil:
			st.State = Stopped
		default:
			st.State = Finished
		}
		update()
	}()

	if err := git.Init(folder); err != nil {
		return err
	}
	if err := git.Exclude(folder, LocalDir+"/"); err != nil {
		return err
	}

	fmt.Fprintf(out, "=== Ralph loop starting ===\nFolder: %s\nPattern: %s\nRuntime: %s\n", folder, s.Pattern, s.Runtime)
	var env backend.Env // nil runs agents on the host
	agentFolder := folder
	if s.Runtime != Host {
		o, err := s.sandboxOptions(folder, out)
		if err != nil {
			return err
		}
		sb, err := sandbox.Start(ctx, o)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		defer sb.Close()
		env, agentFolder = sb, sandbox.Workdir
	}

	agents := map[backend.Role]loop.Agent{}
	for _, role := range s.Pattern.Roles() {
		a := s.Agents[role]
		tmpl, _ := a.template(role) // checked by Validate
		agents[role] = loop.Agent{Backend: backend.New(a.Backend, env), Model: a.Model, Template: tmpl}
		model := a.Model
		if model == "" {
			model = "default model"
		}
		fmt.Fprintf(out, "%s: %s (%s)\n", role, a.Backend, model)
	}
	logDir := filepath.Join(folder, LogDir)
	finished := loop.ResumeRound(logDir, s.Pattern)
	if finished > 0 {
		fmt.Fprintf(out, "Resuming after round %d\n", finished)
	}
	if s.Rounds > 0 {
		st.Until = finished + s.Rounds
		fmt.Fprintf(out, "Running rounds %d to %d\n", finished+1, st.Until)
	}
	fmt.Fprintln(out)

	st.State = Running
	update()
	return loop.Run(ctx, loop.Config{
		Pattern:     s.Pattern,
		Agents:      agents,
		Folder:      folder,
		AgentFolder: agentFolder,
		LogDir:      logDir,
		MaxRounds:   st.Until,
		Output:      out,
		OnStep: func(round int, role backend.Role) {
			st.Round, st.Role = round, role
			update()
		},
	}, finished)
}

// Stop asks the active session on folder, if there is one, to stop after
// tearing down its sandbox.
func Stop(folder string) error {
	st, err := ReadStatus(folder)
	if err != nil || !st.Active() {
		return errors.New("Ralph is not running on this folder")
	}
	p, err := os.FindProcess(st.PID)
	if err != nil {
		return err
	}
	return p.Signal(os.Interrupt)
}

// TurnLog is the transcript of one turn.
type TurnLog struct {
	Round int
	Role  backend.Role
	Path  string // the transcript file
	Done  bool   // whether the turn finished and saved its result
}

// Turns lists the turn transcripts in folder's round logs, in the order
// the turns ran.
func Turns(folder string) []TurnLog {
	dir := filepath.Join(folder, LogDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var turns []TurnLog
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".jsonl")
		if !ok || !strings.HasPrefix(name, "round-") {
			continue
		}
		var t TurnLog
		num, role, ok := strings.Cut(strings.TrimPrefix(name, "round-"), "-")
		if _, err := fmt.Sscanf(num, "%d", &t.Round); err != nil || !ok {
			continue
		}
		t.Role = backend.Role(role)
		if !slices.Contains(Roles, t.Role) {
			continue
		}
		t.Path = filepath.Join(dir, e.Name())
		_, err := os.Stat(filepath.Join(dir, name+".json"))
		t.Done = err == nil
		turns = append(turns, t)
	}
	slices.SortFunc(turns, func(a, b TurnLog) int {
		if a.Round != b.Round {
			return a.Round - b.Round
		}
		return slices.Index(Roles, a.Role) - slices.Index(Roles, b.Role)
	})
	return turns
}
