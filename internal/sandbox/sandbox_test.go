package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// fake is a fake container runtime CLI installed at the front of PATH. It
// logs each invocation and keeps what it is sent on stdin.
type fake struct {
	t   *testing.T
	dir string
}

// installFake installs a fake runtime called name. The image exists if
// imageExists is set; runFails makes container creation fail.
func installFake(t *testing.T, name string, imageExists, runFails bool) *fake {
	t.Helper()
	f := &fake{t: t, dir: t.TempDir()}
	if imageExists {
		f.write("image", "")
	}
	run := "echo id"
	if runFails {
		run = "echo 'Error: no such image' >&2; exit 125"
	}
	script := `#!/bin/sh
d='` + f.dir + `'
{ for a in "$@"; do printf '%s\n' "$a"; done; echo '--'; } >> "$d/log"
case "$1" in
image) [ -f "$d/image" ] || exit 125 ;;
build) cat > "$d/containerfile"; touch "$d/image"; echo built ;;
run) ` + run + ` ;;
exec) [ "$2" = "-i" ] && cat > "$d/stdin-$3" ;;
esac
exit 0
`
	if err := os.WriteFile(filepath.Join(f.dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", f.dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return f
}

func (f *fake) write(name, content string) {
	f.t.Helper()
	if err := os.WriteFile(filepath.Join(f.dir, name), []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fake) read(name string) string {
	f.t.Helper()
	b, err := os.ReadFile(filepath.Join(f.dir, name))
	if err != nil {
		f.t.Fatal(err)
	}
	return string(b)
}

// calls returns the arguments of each invocation of the fake.
func (f *fake) calls() [][]string {
	var calls [][]string
	var cur []string
	for line := range strings.SplitSeq(strings.TrimSuffix(f.read("log"), "\n"), "\n") {
		if line == "--" {
			calls = append(calls, cur)
			cur = nil
			continue
		}
		cur = append(cur, line)
	}
	return calls
}

// subcommands returns the first argument of each invocation.
func (f *fake) subcommands() []string {
	var subs []string
	for _, c := range f.calls() {
		subs = append(subs, c[0])
	}
	return subs
}

func (f *fake) runArgs() [][]string {
	var runs [][]string
	for _, c := range f.calls() {
		if c[0] == "run" {
			runs = append(runs, c)
		}
	}
	return runs
}

func options(t *testing.T, rt string) Options {
	folder := t.TempDir()
	auth := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(auth, []byte(`{"token":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return Options{
		Runtime: rt,
		Folder:  folder,
		Hidden:  []string{".ralph/local"},
		Env:     []string{"RALPH_TEST_KEY", "RALPH_TEST_UNSET"},
		Files:   map[string]string{".codex/auth.json": auth},
	}
}

func TestImage(t *testing.T) {
	if !regexp.MustCompile(`^ralph-agent:[0-9a-f]{12}$`).MatchString(Image()) {
		t.Errorf("Image() = %q", Image())
	}
}

func TestContainerName(t *testing.T) {
	a, b := ContainerName("/a"), ContainerName("/b")
	if a == b || a != ContainerName("/a") || !strings.HasPrefix(a, "ralph-") {
		t.Errorf("ContainerName(/a) = %q, ContainerName(/b) = %q", a, b)
	}
}

func TestStart(t *testing.T) {
	f := installFake(t, "podman", false, false)
	t.Setenv("RALPH_TEST_KEY", "secret")
	o := options(t, "podman")
	var out strings.Builder
	o.Output = &out
	s, err := Start(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.read("containerfile"); got != containerfile {
		t.Errorf("built from %q", got)
	}
	if !strings.Contains(out.String(), "built") {
		t.Errorf("build output not passed on: %q", out.String())
	}
	if got, want := f.subcommands(), []string{"image", "build", "rm", "run", "exec", "run", "exec"}; !slices.Equal(got, want) {
		t.Errorf("subcommands = %v, want %v", got, want)
	}

	name := ContainerName(o.Folder)
	runs := f.runArgs()
	for i, c := range []struct{ suffix, mount string }{{"-rw", ""}, {"-ro", ":ro"}} {
		want := []string{"run", "-d", "--init", "--name", name + c.suffix, "--label", "ralph.folder=" + o.Folder, "-v", o.Folder + ":/work" + c.mount}
		if runtime.GOOS == "linux" {
			want = append(want, "--userns=keep-id:uid=1000,gid=1000")
		}
		want = append(want, "--mount", "type=tmpfs,destination=/work/.ralph/local,notmpcopyup", "-e", "RALPH_TEST_KEY", Image(), "sleep", "infinity")
		if !slices.Equal(runs[i], want) {
			t.Errorf("run:\n got %q\nwant %q", runs[i], want)
		}
	}
	if strings.Contains(f.read("log"), "secret") {
		t.Error("an environment variable's value appears in the arguments")
	}
	for _, suffix := range []string{"-rw", "-ro"} {
		if got := f.read("stdin-" + name + suffix); got != `{"token":"x"}` {
			t.Errorf("copied %q into %s", got, suffix)
		}
	}

	cmd := s.Command(context.Background(), true, "/work", "claude", "-p", "hi")
	if want := []string{"exec", "-w", "/work", name + "-ro", "claude", "-p", "hi"}; !slices.Equal(cmd.Args[1:], want) {
		t.Errorf("read-only command = %q, want %q", cmd.Args[1:], want)
	}
	cmd = s.Command(context.Background(), false, "/work", "codex")
	if cmd.Args[4] != name+"-rw" {
		t.Errorf("writable command runs in %s", cmd.Args[4])
	}
	if !s.Isolated() {
		t.Error("a sandbox is not isolated")
	}

	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	calls := f.calls()
	if got, want := calls[len(calls)-1], []string{"rm", "-f", name + "-rw", name + "-ro"}; !slices.Equal(got, want) {
		t.Errorf("Close ran %q, want %q", got, want)
	}
}

func TestStartImageExists(t *testing.T) {
	f := installFake(t, "docker", true, false)
	o := options(t, "docker")
	o.Files = nil
	if _, err := Start(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	if got, want := f.subcommands(), []string{"image", "rm", "run", "run"}; !slices.Equal(got, want) {
		t.Errorf("subcommands = %v, want %v", got, want)
	}
	// Docker's tmpfs mounts start empty and take no copy-up option.
	if !slices.Contains(f.runArgs()[0], "type=tmpfs,destination=/work/.ralph/local") {
		t.Errorf("run args = %q", f.runArgs()[0])
	}
}

func TestStartRunFails(t *testing.T) {
	f := installFake(t, "podman", true, true)
	_, err := Start(context.Background(), options(t, "podman"))
	if err == nil || !strings.Contains(err.Error(), "no such image") {
		t.Errorf("err = %v, want the runtime's message", err)
	}
	if subs := f.subcommands(); subs[len(subs)-1] != "rm" {
		t.Errorf("containers not removed after a failure: %v", subs)
	}
}

func TestStartMissingFile(t *testing.T) {
	installFake(t, "podman", true, false)
	o := options(t, "podman")
	o.Files = map[string]string{"x": filepath.Join(t.TempDir(), "missing")}
	if _, err := Start(context.Background(), o); !os.IsNotExist(err) {
		t.Errorf("err = %v, want a not-exist error", err)
	}
}

func TestStartBuildFails(t *testing.T) {
	f := installFake(t, "podman", false, false)
	// Replace the fake with one whose build fails.
	script := "#!/bin/sh\n[ \"$1\" = build ] && { cat >/dev/null; exit 1; }\nexit 125\n"
	if err := os.WriteFile(filepath.Join(f.dir, "podman"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Start(context.Background(), options(t, "podman")); err == nil || !strings.Contains(err.Error(), "building the agent image") {
		t.Errorf("err = %v", err)
	}
}

func TestEnsureImage(t *testing.T) {
	f := installFake(t, "podman", false, false)
	var out strings.Builder
	if err := EnsureImage(context.Background(), "podman", &out); err != nil {
		t.Fatal(err)
	}
	if err := EnsureImage(context.Background(), "podman", nil); err != nil {
		t.Fatal(err)
	}
	if got, want := f.subcommands(), []string{"image", "build", "image"}; !slices.Equal(got, want) {
		t.Errorf("subcommands = %v, want %v: the image built more than once", got, want)
	}
	if !strings.Contains(out.String(), "Building the agent image") {
		t.Errorf("output = %q", out.String())
	}
}

func TestStartUnknownRuntime(t *testing.T) {
	if _, err := Start(context.Background(), Options{Runtime: "lxc"}); err == nil {
		t.Error("expected an error")
	}
}

func TestStartRuntimeNotInstalled(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	saved := fallbackDirs
	fallbackDirs = nil
	defer func() { fallbackDirs = saved }()
	if _, err := Start(context.Background(), Options{Runtime: "docker"}); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Errorf("err = %v", err)
	}
	if got := Detect(); got != "" {
		t.Errorf("Detect() = %q with no runtime installed", got)
	}
}

func TestLookRuntimeFallback(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	dir := t.TempDir()
	saved := fallbackDirs
	fallbackDirs = []string{t.TempDir(), dir}
	defer func() { fallbackDirs = saved }()
	bin := filepath.Join(dir, "podman")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := LookRuntime("podman"); err != nil || got != bin {
		t.Errorf("LookRuntime = %q, %v; want %q", got, err, bin)
	}
	if got := Detect(); got != "podman" {
		t.Errorf("Detect() = %q, want podman", got)
	}
}
