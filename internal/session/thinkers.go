package session

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/changkun/ralph/internal/backend"
	"github.com/changkun/ralph/internal/prompt"
	"github.com/changkun/ralph/internal/sandbox"
)

// The thinkers pattern runs only the strategist of think+act, with its
// settings and prompt, as thinkers side by side. Each runs once, in a new
// empty folder of its own, thinkerN in the project folder, and in a
// sandbox that sees only that folder, read-only as the strategist always
// does. None knows of the others. Their transcripts and final answers go
// in LogDir as thinkerN.jsonl and thinkerN.json, out of every thinker's
// sight. Each start runs more thinkers, numbered after those before.

// DefaultThinkers is how many thinkers a start runs by default, and
// MaxThinkers how many it can run.
const (
	DefaultThinkers = 3
	MaxThinkers     = 20
)

// ThinkerFolder returns the name of thinker n's folder in the project
// folder.
func ThinkerFolder(n int) string { return "thinker" + strconv.Itoa(n) }

func thinkerLog(folder string, n int, ext string) string {
	return filepath.Join(folder, LogDir, ThinkerFolder(n)+ext)
}

// thinkerResult is what a thinker's .json log holds once it has finished.
type thinkerResult struct {
	Result string `json:"result"`          // its final answer
	Error  string `json:"error,omitempty"` // why it failed, if it did
}

// runThinkers runs s.Thinkers thinkers side by side, numbered after those
// that ran in folder before, and records their numbers in st. It returns
// an error if any of them failed, once all have finished.
func runThinkers(ctx context.Context, folder string, s Settings, st *Status, update func(), out io.Writer) error {
	out = &syncWriter{w: out}
	a := s.Agents[backend.Strategist]
	tmpl, _ := a.template(backend.Strategist) // checked by Validate
	p, err := tmpl.Render(prompt.Round{Folder: sandbox.Workdir})
	if err != nil {
		return fmt.Errorf("%s: %w", backend.Strategist, err)
	}
	o, err := s.sandboxOptions(folder, out)
	if err != nil {
		return err
	}
	o.Hidden = nil // a thinker's folder has nothing to hide

	first := NextThinker(folder)
	for n := first; n < first+s.Thinkers; n++ {
		// A new folder, so that the thinker starts with nothing.
		if err := os.Mkdir(filepath.Join(folder, ThinkerFolder(n)), 0o755); err != nil {
			return err
		}
		st.Thinkers = append(st.Thinkers, n)
	}
	model := a.Model
	if model == "" {
		model = "default model"
	}
	fmt.Fprintf(out, "=== Ralph thinkers starting ===\nFolder: %s\nRuntime: %s\nThinkers %d to %d, each a strategist alone: %s (%s)\n\n",
		folder, s.Runtime, first, first+s.Thinkers-1, a.Backend, model)
	// The sandboxes start side by side, so the image is built first, once.
	if err := sandbox.EnsureImage(ctx, s.Runtime, out); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	st.State = Running
	update()

	errs := make([]error, len(st.Thinkers))
	var wg sync.WaitGroup
	for i, n := range st.Thinkers {
		wg.Go(func() { errs[i] = runThinker(ctx, folder, n, o, a, p, out) })
	}
	wg.Wait()
	if ctx.Err() != nil {
		return nil
	}
	failed := 0
	for _, err := range errs {
		if err != nil {
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d thinkers failed", failed, len(errs))
	}
	return nil
}

// runThinker runs thinker n, a strategist with prompt p, in a sandbox like
// o that sees only the thinker's folder, and saves its transcript and its
// final answer or failure. A thinker that is stopped saves neither.
func runThinker(ctx context.Context, folder string, n int, o sandbox.Options, a Agent, p prompt.Prompt, out io.Writer) (err error) {
	name := ThinkerFolder(n)
	// The transcript exists from the start, so that the thinker is listed
	// while its sandbox starts.
	f, err := os.Create(thinkerLog(folder, n, ".jsonl"))
	if err != nil {
		return err
	}
	defer f.Close()
	var result string
	defer func() {
		if ctx.Err() != nil {
			return
		}
		r := thinkerResult{Result: result}
		if err != nil {
			r.Error = err.Error()
			fmt.Fprintf(out, "[%s] Failed: %v\n", name, err)
		} else {
			fmt.Fprintf(out, "[%s] Done. Its answer:\n\n%s\n\n", name, strings.TrimSpace(result))
		}
		data, _ := json.Marshal(r)
		if werr := writeFile(thinkerLog(folder, n, ".json"), data); werr != nil && err == nil {
			err = werr
		}
	}()

	fmt.Fprintf(out, "[%s] Starting its container.\n", name)
	o.Folder = filepath.Join(folder, name)
	sb, err := sandbox.Start(ctx, o)
	if err != nil {
		return err
	}
	defer sb.Close()
	fmt.Fprintf(out, "[%s] Thinking.\n", name)
	result, err = backend.New(a.Backend, sb).Run(ctx, backend.Turn{
		Role:       backend.Strategist, // so it cannot change its folder
		Folder:     sandbox.Workdir,
		Model:      a.Model,
		Prompt:     p,
		Transcript: f,
	})
	return err
}

// thinkerNumber returns n if name is thinkerN, with ext after it.
func thinkerNumber(name, ext string) (int, bool) {
	rest, ok := strings.CutPrefix(name, "thinker")
	if !ok {
		return 0, false
	}
	if rest, ok = strings.CutSuffix(rest, ext); !ok {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	return n, err == nil && n > 0 && strconv.Itoa(n) == rest
}

// NextThinker returns the number of the next thinker to run in folder: one
// more than the highest of the thinkers' folders and logs there.
func NextThinker(folder string) int {
	last := 0
	for _, dir := range []string{folder, filepath.Join(folder, LogDir)} {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			for _, ext := range []string{"", ".json", ".jsonl"} {
				if n, ok := thinkerNumber(e.Name(), ext); ok && n > last {
					last = n
				}
			}
		}
	}
	return last + 1
}

// ThinkerLogs lists the thinkers that ran in folder, in order. Each one's
// Round is its number.
func ThinkerLogs(folder string) []TurnLog {
	entries, err := os.ReadDir(filepath.Join(folder, LogDir))
	if err != nil {
		return nil
	}
	var nums []int
	for _, e := range entries {
		for _, ext := range []string{".json", ".jsonl"} {
			if n, ok := thinkerNumber(e.Name(), ext); ok && !slices.Contains(nums, n) {
				nums = append(nums, n)
			}
		}
	}
	slices.Sort(nums)
	turns := make([]TurnLog, len(nums))
	for i, n := range nums {
		t := TurnLog{Round: n, Role: backend.Strategist, Thinker: true, Path: thinkerLog(folder, n, ".jsonl")}
		if data, err := os.ReadFile(thinkerLog(folder, n, ".json")); err == nil {
			var r thinkerResult
			if json.Unmarshal(data, &r) == nil {
				t.Result, t.Error, t.Done = r.Result, r.Error, r.Error == ""
			}
		}
		turns[i] = t
	}
	return turns
}

// syncWriter serializes writes to w, for thinkers running side by side.
type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (w *syncWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.w.Write(p)
}
