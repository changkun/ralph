// Package sandbox runs agent CLIs in containers that see nothing of this
// machine but the project folder.
//
// A sandbox is a pair of containers made from the agent image (see
// Containerfile): one mounts the project folder writable and one mounts it
// read-only, both at Workdir. Turns of read-only roles run in the second.
package sandbox

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"
)

//go:embed Containerfile
var containerfile string

// Workdir is where containers mount the project folder.
const Workdir = "/work"

// Runtimes lists the supported container runtimes, the default first.
var Runtimes = []string{"podman", "docker"}

// Image returns the tag of the agent image built from the Containerfile.
func Image() string {
	sum := sha256.Sum256([]byte(containerfile))
	return "ralph-agent:" + hex.EncodeToString(sum[:])[:12]
}

// Options describes a sandbox.
type Options struct {
	Runtime string // the container runtime, one of Runtimes
	Folder  string // the absolute path of the project folder on this machine

	// Hidden lists folders, relative to Folder, that agents must not see.
	// Each must exist, so that a read-only mount can be masked over it.
	Hidden []string

	// Env names environment variables, such as API keys, to pass to agents.
	// Those not set here are skipped. Only names appear on command lines.
	Env []string

	// Files maps paths in the agents' home directory to files on this
	// machine that are copied there, such as a CLI's login.
	Files map[string]string

	// Output receives the output of building the image; nil discards it.
	Output io.Writer
}

// Sandbox is a running pair of agent containers. It implements
// backend.Env.
type Sandbox struct {
	runtime        string // path of the runtime CLI
	writer, reader string // container names
}

// Start starts the containers for o.Folder, building the agent image first
// if it does not exist, and removing containers left over from an earlier
// sandbox for the same folder.
func Start(ctx context.Context, o Options) (*Sandbox, error) {
	if !slices.Contains(Runtimes, o.Runtime) {
		return nil, fmt.Errorf("unknown container runtime %q; want %s", o.Runtime, strings.Join(Runtimes, " or "))
	}
	bin, err := LookRuntime(o.Runtime)
	if err != nil {
		return nil, err
	}
	if o.Output == nil {
		o.Output = io.Discard
	}
	if err := ensureImage(ctx, bin, o.Output); err != nil {
		return nil, err
	}
	name := ContainerName(o.Folder)
	s := &Sandbox{runtime: bin, writer: name + "-rw", reader: name + "-ro"}
	s.remove()
	for _, c := range []struct {
		name     string
		readOnly bool
	}{{s.writer, false}, {s.reader, true}} {
		if err := s.start(ctx, o, c.name, c.readOnly); err != nil {
			s.Close()
			return nil, err
		}
	}
	return s, nil
}

// ContainerName returns the name prefix of the containers for folder.
func ContainerName(folder string) string {
	sum := sha256.Sum256([]byte(folder))
	return "ralph-" + hex.EncodeToString(sum[:])[:8]
}

// fallbackDirs are where runtimes are commonly installed, for when this
// process does not have a login shell's PATH, as when a GUI app is opened
// from the macOS Finder.
var fallbackDirs = []string{
	"/opt/homebrew/bin",
	"/usr/local/bin",
	"/opt/podman/bin",
	"/Applications/Docker.app/Contents/Resources/bin",
}

// LookRuntime returns the path of the runtime CLI called name.
func LookRuntime(name string) (string, error) {
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	for _, dir := range fallbackDirs {
		p := filepath.Join(dir, name)
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() && fi.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", fmt.Errorf("%s is not installed, or not on PATH", name)
}

// Detect returns the first of Runtimes that is installed, or "" if none is.
func Detect() string {
	for _, name := range Runtimes {
		if _, err := LookRuntime(name); err == nil {
			return name
		}
	}
	return ""
}

// EnsureImage builds the agent image with runtime, one of Runtimes, if it
// does not exist, printing the build's output to out. Start does so too,
// but sandboxes that start side by side need it done once first.
func EnsureImage(ctx context.Context, runtime string, out io.Writer) error {
	bin, err := LookRuntime(runtime)
	if err != nil {
		return err
	}
	if out == nil {
		out = io.Discard
	}
	return ensureImage(ctx, bin, out)
}

func ensureImage(ctx context.Context, bin string, out io.Writer) error {
	if exec.CommandContext(ctx, bin, "image", "inspect", "--format", "{{.Id}}", Image()).Run() == nil {
		return nil
	}
	fmt.Fprintf(out, "Building the agent image %s; this takes a few minutes the first time.\n", Image())
	cmd := exec.CommandContext(ctx, bin, "build", "-t", Image(), "-")
	cmd.Stdin = strings.NewReader(containerfile)
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("building the agent image: %w", err)
	}
	return nil
}

func (s *Sandbox) start(ctx context.Context, o Options, name string, readOnly bool) error {
	mount := o.Folder + ":" + Workdir
	if readOnly {
		mount += ":ro"
	}
	args := []string{"run", "-d", "--init", "--name", name, "--label", "ralph.folder=" + o.Folder, "-v", mount}
	if o.Runtime == "podman" && runtime.GOOS == "linux" {
		// Map this user to the image's user, so that files the agents
		// create belong to this user.
		args = append(args, "--userns=keep-id:uid=1000,gid=1000")
	}
	for _, h := range o.Hidden {
		m := "type=tmpfs,destination=" + path.Join(Workdir, filepath.ToSlash(h))
		if o.Runtime == "podman" {
			// Podman otherwise copies what the mount hides into it.
			m += ",notmpcopyup"
		}
		args = append(args, "--mount", m)
	}
	for _, env := range o.Env {
		if _, ok := os.LookupEnv(env); ok {
			args = append(args, "-e", env)
		}
	}
	args = append(args, Image(), "sleep", "infinity")
	if err := s.run(ctx, nil, args...); err != nil {
		return err
	}
	dsts := make([]string, 0, len(o.Files))
	for dst := range o.Files {
		dsts = append(dsts, dst)
	}
	slices.Sort(dsts)
	for _, dst := range dsts {
		if err := s.copyFile(ctx, name, o.Files[dst], dst); err != nil {
			return err
		}
	}
	return nil
}

// copyFile copies src on this machine to dst, relative to the agent's home
// directory, in container name, readable only by the agent.
func (s *Sandbox) copyFile(ctx context.Context, name, src, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	const script = `cd && mkdir -p "$(dirname "$1")" && umask 077 && cat > "$1"`
	return s.run(ctx, f, "exec", "-i", name, "sh", "-c", script, "sh", dst)
}

// run runs the runtime CLI and includes its output in any error.
func (s *Sandbox) run(ctx context.Context, stdin io.Reader, args ...string) error {
	cmd := exec.CommandContext(ctx, s.runtime, args...)
	cmd.Stdin = stdin
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(out.String()); msg != "" {
			return fmt.Errorf("%s %s: %w: %s", filepath.Base(s.runtime), args[0], err, msg)
		}
		return fmt.Errorf("%s %s: %w", filepath.Base(s.runtime), args[0], err)
	}
	return nil
}

// Command implements backend.Env. It runs the command in the read-only
// container if readOnly is set, and in the writable one otherwise.
//
// Cancelling ctx stops the runtime CLI but not necessarily the command in
// the container; Close stops everything.
func (s *Sandbox) Command(ctx context.Context, readOnly bool, dir, name string, args ...string) *exec.Cmd {
	ctr := s.writer
	if readOnly {
		ctr = s.reader
	}
	return exec.CommandContext(ctx, s.runtime, append([]string{"exec", "-w", dir, ctr, name}, args...)...)
}

// Isolated implements backend.Env.
func (*Sandbox) Isolated() bool { return true }

// Close stops and removes the containers and everything running in them.
func (s *Sandbox) Close() error { return s.remove() }

func (s *Sandbox) remove() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return s.run(ctx, nil, "rm", "-f", s.writer, s.reader)
}
