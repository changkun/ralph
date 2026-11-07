package session

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/changkun/ralph/internal/backend"
	"github.com/changkun/ralph/internal/loop"
)

// fakeThinkerBin installs a fake podman, which runs exec'd commands on the
// host in the folder the container mounts, and a fake claude, which logs
// where it ran and how many files it found there, keeps its arguments, and
// answers with an idea named after its folder. The thinker whose folder is
// called fail fails.
func fakeThinkerBin(t *testing.T, fail string) string {
	t.Helper()
	return fakeBin(t, map[string]string{
		"podman": `d=$(dirname "$0")
echo "$*" >> "$d/podman.log" # one write, as runs go side by side
case "$1" in
run) while [ $# -gt 0 ]; do case "$1" in --name) n=$2 ;; -v) v=$2 ;; esac; shift; done
  echo "${v%%:*}" > "$d/$n.dir"; echo id ;;
exec) shift 3; c=$1; shift; cd "$(cat "$d/$c.dir")" && exec "$@" ;;
esac`,
		"claude": `d=$(dirname "$0")
here=$(basename "$(pwd)")
echo "$(pwd) $(ls -A | wc -l | tr -d ' ')" >> "$d/claude.log"
printf '%s\n' "$@" > "$d/args-$here"
[ "$here" = '` + fail + `' ] && { echo 'Not logged in' >&2; exit 1; }
echo '{"type":"result","result":"an idea from '"$here"'"}'`,
	})
}

func thinkerSettings(n int) Settings {
	s := Defaults()
	s.Pattern, s.Runtime, s.Thinkers = loop.Thinkers, "podman", n
	return s
}

func TestRunThinkers(t *testing.T) {
	isolate(t)
	t.Setenv("ANTHROPIC_API_KEY", "secret")
	bin := fakeThinkerBin(t, "")
	folder := t.TempDir()
	// The engine runs thinkers in a folder that is not a repository.
	if err := thinkerSettings(3).Save(folder); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := Engine(folder, &out); err != nil {
		t.Fatal(err)
	}

	// Each thinker ran once, in an empty folder of its own.
	claude := read(t, filepath.Join(bin, "claude.log"))
	for n := 1; n <= 3; n++ {
		if want := filepath.Join(folder, ThinkerFolder(n)) + " 0\n"; !strings.Contains(claude, want) {
			t.Errorf("thinker %d did not run alone in an empty folder:\n%s", n, claude)
		}
	}
	if c := strings.Count(claude, "\n"); c != 3 {
		t.Errorf("%d turns ran, want 3:\n%s", c, claude)
	}
	// Each sandbox mounted its thinker's folder and nothing else.
	var mounts []string
	for _, line := range strings.Split(read(t, filepath.Join(bin, "podman.log")), "\n") {
		args := strings.Fields(line)
		for i, arg := range args {
			if (arg == "-v" || arg == "--mount") && i+1 < len(args) {
				mounts = append(mounts, args[i+1])
			}
		}
	}
	if len(mounts) != 6 {
		t.Errorf("mounts = %q, want a pair of containers for each thinker", mounts)
	}
	for _, m := range mounts {
		dir, _, _ := strings.Cut(m, ":")
		if n, ok := thinkerNumber(filepath.Base(dir), ""); !ok || filepath.Dir(dir) != folder || n > 3 {
			t.Errorf("a sandbox mounted %q", m)
		}
	}
	if _, err := os.Stat(filepath.Join(folder, ".git")); err == nil {
		t.Error("the project folder was made a repository")
	}

	logs := ThinkerLogs(folder)
	if len(logs) != 3 {
		t.Fatalf("thinker logs = %+v", logs)
	}
	for i, l := range logs {
		name := ThinkerFolder(i + 1)
		if l.Round != i+1 || !l.Done || l.Result != "an idea from "+name || l.Error != "" {
			t.Errorf("thinker log = %+v", l)
		}
		if _, err := os.Stat(l.Path); err != nil {
			t.Errorf("no transcript: %v", err)
		}
		// A thinker is the strategist, with its prompt.
		if !l.Thinker || l.Role != backend.Strategist {
			t.Errorf("thinker log = %+v", l)
		}
		strategist := DefaultAgent(backend.Strategist).Prompt
		if args := read(t, filepath.Join(bin, "args-"+name)); !strings.Contains(args, strategist) {
			t.Errorf("%s was not given the strategist's prompt:\n%s", name, args)
		}
	}
	// Each ran in its read-only container.
	for _, line := range strings.Split(read(t, filepath.Join(bin, "podman.log")), "\n") {
		if args := strings.Fields(line); len(args) > 3 && args[0] == "exec" && !strings.HasSuffix(args[3], "-ro") {
			t.Errorf("a thinker ran in a writable container: %s", line)
		}
	}
	if st, _ := ReadStatus(folder); st.State != Finished || !slices.Equal(st.Thinkers, []int{1, 2, 3}) {
		t.Errorf("status = %+v", st)
	}
	if !strings.Contains(out.String(), "Thinkers 1 to 3") || !strings.Contains(out.String(), "[thinker2] Done.") {
		t.Errorf("output:\n%s", out.String())
	}

	// Starting again runs more thinkers, numbered after those before.
	if err := Run(context.Background(), folder, thinkerSettings(2), &out); err != nil {
		t.Fatal(err)
	}
	if logs := ThinkerLogs(folder); len(logs) != 5 || logs[4].Result != "an idea from thinker5" {
		t.Errorf("thinker logs = %+v", logs)
	}
	if st, _ := ReadStatus(folder); !slices.Equal(st.Thinkers, []int{4, 5}) {
		t.Errorf("status = %+v", st)
	}
}

func TestRunThinkersFailure(t *testing.T) {
	isolate(t)
	t.Setenv("ANTHROPIC_API_KEY", "x")
	fakeThinkerBin(t, "thinker2")
	folder := t.TempDir()
	err := Run(context.Background(), folder, thinkerSettings(3), &strings.Builder{})
	if err == nil || err.Error() != "1 of 3 thinkers failed" {
		t.Errorf("err = %v", err)
	}
	logs := ThinkerLogs(folder)
	if len(logs) != 3 || !logs[0].Done || !logs[2].Done {
		t.Fatalf("thinker logs = %+v", logs)
	}
	if l := logs[1]; l.Done || !strings.Contains(l.Error, "Not logged in") {
		t.Errorf("failed thinker = %+v", l)
	}
	if st, _ := ReadStatus(folder); st.State != Failed {
		t.Errorf("status = %+v", st)
	}
}

func TestRunThinkersNeedsLogin(t *testing.T) {
	isolate(t)
	fakeThinkerBin(t, "")
	folder := t.TempDir()
	err := Run(context.Background(), folder, thinkerSettings(2), &strings.Builder{})
	if err == nil || !strings.Contains(err.Error(), "no login") {
		t.Errorf("err = %v", err)
	}
	if n := NextThinker(folder); n != 1 {
		t.Errorf("thinker folders were made without a login: next is %d", n)
	}
}

func TestNextThinker(t *testing.T) {
	folder := t.TempDir()
	if n := NextThinker(folder); n != 1 {
		t.Errorf("next = %d, want 1", n)
	}
	for _, dir := range []string{"thinker2", "thinker03", "thinkers", "thinker9x"} {
		if err := os.MkdirAll(filepath.Join(folder, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if n := NextThinker(folder); n != 3 {
		t.Errorf("next = %d, want 3", n)
	}
	if err := writeFile(filepath.Join(folder, LogDir, "thinker7.json"), []byte(`{"error":"x"}`)); err != nil {
		t.Fatal(err)
	}
	if n := NextThinker(folder); n != 8 {
		t.Errorf("next = %d, want 8", n)
	}
	if logs := ThinkerLogs(folder); len(logs) != 1 || logs[0].Round != 7 || logs[0].Done || logs[0].Error != "x" {
		t.Errorf("thinker logs = %+v", logs)
	}
}
