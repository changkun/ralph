// Package git persists Ralph's rounds by committing and pushing the project.
package git

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// IsRepo reports whether folder is inside a git work tree.
func IsRepo(folder string) bool {
	return command(folder, "rev-parse", "--is-inside-work-tree").Run() == nil
}

// Init creates an empty repository at folder unless folder is already in a
// git work tree.
func Init(folder string) error {
	if IsRepo(folder) {
		return nil
	}
	return run(folder, "init", "-q")
}

// Exclude adds pattern to the repository's local exclude file, which works
// like .gitignore but is not part of the project, unless it is there
// already. The repository then ignores the paths pattern matches.
func Exclude(folder, pattern string) error {
	out, err := command(folder, "rev-parse", "--git-path", "info/exclude").Output()
	if err != nil {
		return fmt.Errorf("git rev-parse: %w", err)
	}
	path := strings.TrimSpace(string(out))
	if !filepath.IsAbs(path) {
		path = filepath.Join(folder, path)
	}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	for sc := bufio.NewScanner(bytes.NewReader(data)); sc.Scan(); {
		if strings.TrimSpace(sc.Text()) == pattern {
			return nil
		}
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		pattern = "\n" + pattern
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(pattern + "\n"); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// HasChanges reports whether the repository at folder has staged, unstaged,
// or untracked changes.
func HasChanges(folder string) bool {
	out, err := command(folder, "status", "--porcelain").Output()
	return err == nil && len(strings.TrimSpace(string(out))) > 0
}

// HasUpstream reports whether the current branch has an upstream configured.
func HasUpstream(folder string) bool {
	return command(folder, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}").Run() == nil
}

// CommitAll stages every change in the repository, commits it with message,
// and pushes if the current branch has an upstream. It reports whether a
// commit was made and whether it was pushed. If there is nothing to commit,
// it does nothing. When err is not nil but committed is true, the commit was
// made and only the push failed.
func CommitAll(folder, message string) (committed, pushed bool, err error) {
	if err := run(folder, "add", "-A"); err != nil {
		return false, false, err
	}
	if !hasStagedChanges(folder) {
		return false, false, nil
	}
	if err := run(folder, "commit", "-m", message); err != nil {
		return false, false, err
	}
	if !HasUpstream(folder) {
		return true, false, nil
	}
	if err := run(folder, "push"); err != nil {
		return true, false, err
	}
	return true, true, nil
}

func hasStagedChanges(folder string) bool {
	// git diff --quiet exits with status 1 when there are differences.
	return command(folder, "diff", "--cached", "--quiet").Run() != nil
}

func command(folder string, args ...string) *exec.Cmd {
	return exec.Command("git", append([]string{"-C", folder}, args...)...)
}

// run runs a git command and includes its output in any error.
func run(folder string, args ...string) error {
	out, err := command(folder, args...).CombinedOutput()
	if err == nil {
		return nil
	}
	if msg := strings.TrimSpace(string(out)); msg != "" {
		return fmt.Errorf("git %s: %w: %s", args[0], err, msg)
	}
	return fmt.Errorf("git %s: %w", args[0], err)
}
