package git

import (
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