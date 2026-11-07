package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitCmd(t, dir, "init", "-q")
	gitCmd(t, dir, "config", "user.email", "t@t")
	gitCmd(t, dir, "config", "user.name", "T")
	gitCmd(t, dir, "config", "commit.gpgsign", "false")
	return dir
}

// initRepoWithUpstream returns a repository whose current branch tracks a
// branch in a bare remote repository, and the remote's path.
func initRepoWithUpstream(t *testing.T) (dir, remote string) {
	t.Helper()
	dir = initRepo(t)
	remote = t.TempDir()
	gitCmd(t, remote, "init", "-q", "--bare")
	writeFile(t, dir, "init.txt")
	gitCmd(t, dir, "add", ".")
	gitCmd(t, dir, "commit", "-q", "-m", "init")
	gitCmd(t, dir, "remote", "add", "origin", remote)
	gitCmd(t, dir, "push", "-q", "-u", "origin", "HEAD")
	return dir, remote
}

func writeFile(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIsRepo(t *testing.T) {
	if IsRepo(t.TempDir()) {
		t.Error("plain directory reported as a repository")
	}
	if !IsRepo(initRepo(t)) {
		t.Error("repository not detected")
	}
}

func TestHasChanges(t *testing.T) {
	dir := initRepo(t)
	if HasChanges(dir) {
		t.Error("clean repository reported changes")
	}
	writeFile(t, dir, "file.txt")
	if !HasChanges(dir) {
		t.Error("untracked file not reported as a change")
	}
	if HasChanges("/nonexistent_ralph_test") {
		t.Error("missing directory reported changes")
	}
}

func TestHasUpstream(t *testing.T) {
	if HasUpstream(initRepo(t)) {
		t.Error("repository without a remote reported an upstream")
	}
	dir, _ := initRepoWithUpstream(t)
	if !HasUpstream(dir) {
		t.Error("tracking branch not reported as an upstream")
	}
}

func TestCommitAllWithoutUpstream(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "file.txt")
	committed, pushed, err := CommitAll(dir, "test commit")
	if err != nil {
		t.Fatal(err)
	}
	if !committed || pushed {
		t.Errorf("committed=%v pushed=%v, want true false", committed, pushed)
	}
	if HasChanges(dir) {
		t.Error("working tree not clean after commit")
	}
}

func TestCommitAllNothingToCommit(t *testing.T) {
	committed, pushed, err := CommitAll(initRepo(t), "test commit")
	if err != nil || committed || pushed {
		t.Errorf("committed=%v pushed=%v err=%v, want false false nil", committed, pushed, err)
	}
}

func TestCommitAllPushes(t *testing.T) {
	dir, remote := initRepoWithUpstream(t)
	writeFile(t, dir, "file.txt")
	committed, pushed, err := CommitAll(dir, "test commit")
	if err != nil {
		t.Fatal(err)
	}
	if !committed || !pushed {
		t.Errorf("committed=%v pushed=%v, want true true", committed, pushed)
	}
	if log := gitCmd(t, remote, "log", "--format=%s"); !strings.Contains(log, "test commit") {
		t.Errorf("remote log does not contain the commit:\n%s", log)
	}
}

func TestCommitAllPushFails(t *testing.T) {
	dir, remote := initRepoWithUpstream(t)
	if err := os.RemoveAll(remote); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "file.txt")
	committed, pushed, err := CommitAll(dir, "test commit")
	if err == nil || !strings.Contains(err.Error(), "git push") {
		t.Errorf("err = %v, want a push error", err)
	}
	if !committed || pushed {
		t.Errorf("committed=%v pushed=%v, want true false", committed, pushed)
	}
}

func TestCommitAllNotARepo(t *testing.T) {
	committed, _, err := CommitAll(t.TempDir(), "test commit")
	if err == nil || committed {
		t.Errorf("committed=%v err=%v, want false and an error", committed, err)
	}
}
