package git

import (
	"fmt"
	"os/exec"
	"strings"
)

// IsRepo returns true if folder is inside a git work tree.
func IsRepo(folder string) bool {
	cmd := exec.Command("git", "-C", folder, "rev-parse", "--is-inside-work-tree")
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}

// HasChanges returns true if the git repo has staged, unstaged, or untracked changes.
func HasChanges(folder string) bool {
	cmd := exec.Command("git", "-C", folder, "status", "--porcelain")
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(out))) > 0
}

// CommitAll stages all repository changes, creates a commit, and pushes if an
// upstream is configured. It returns whether a commit was created and whether a
// push was attempted.
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
	return true, true, run(folder, "push")
}

// HasUpstream reports whether the current branch has an upstream configured.
func HasUpstream(folder string) bool {
	cmd := exec.Command("git", "-C", folder, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run() == nil
}

func hasStagedChanges(folder string) bool {
	cmd := exec.Command("git", "-C", folder, "diff", "--cached", "--quiet")
	err := cmd.Run()
	return err != nil
}

func run(folder string, args ...string) error {
	cmd := exec.Command("git", append([]string{"-C", folder}, args...)...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	msg := strings.TrimSpace(string(out))
	if msg == "" {
		return err
	}
	return fmt.Errorf("%w: %s", err, msg)
}
