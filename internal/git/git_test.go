package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, a := range [][]string{
		{"init"}, {"config", "user.email", "t@t"}, {"config", "user.name", "T"},
	} {
		if err := exec.Command("git", append([]string{"-C", dir}, a...)...).Run(); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestIsRepo(t *testing.T) {
	dir := t.TempDir()
	if IsRepo(dir) {
		t.Error("non-git dir should return false")
	}
	dir = initRepo(t)
	if !IsRepo(dir) {
		t.Error("git dir should return true")
	}
}

func TestHasChanges(t *testing.T) {
	dir := initRepo(t)
	if HasChanges(dir) {
		t.Error("clean repo should have no changes")
	}
	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hello"), 0o644)
	if !HasChanges(dir) {
		t.Error("repo with new file should have changes")
	}
}

func TestHasChangesNonGit(t *testing.T) {
	if HasChanges("/nonexistent_ralph_test") {
		t.Error("should return false")
	}
}

func TestHasUpstream(t *testing.T) {
	dir := initRepo(t)
	if HasUpstream(dir) {
		t.Error("repo without remote should not have upstream")
	}
}

func TestCommitAllWithoutUpstream(t *testing.T) {
	dir := initRepo(t)
	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hello"), 0o644)
	committed, pushed, err := CommitAll(dir, "test commit")
	if err != nil {
		t.Fatal(err)
	}
	if !committed {
		t.Fatal("expected commit to be created")
	}
	if pushed {
		t.Fatal("expected push to be skipped without upstream")
	}
	if HasChanges(dir) {
		t.Fatal("expected clean working tree after commit")
	}
}
