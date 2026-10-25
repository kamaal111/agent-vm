package project_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agent-vm/internal/host"
	"agent-vm/internal/project"
	u "agent-vm/internal/testutil"
)

func TestGitWorktreeSnapshotPreservesEditsWithoutHostGitPaths(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "main")
	u.OK(t, os.Mkdir(repo, 0700))
	u.Git(t, repo, "init")
	u.Write(t, filepath.Join(repo, "edited.txt"), "before")
	u.Write(t, filepath.Join(repo, "deleted.txt"), "delete me")
	u.Write(t, filepath.Join(repo, "binary"), "\x00before\xff")
	u.Write(t, filepath.Join(repo, ".gitignore"), "ignored\n")
	u.Git(t, repo, "add", ".")
	u.Git(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.test", "-c", "commit.gpgsign=false", "commit", "-m", "base")
	wt := filepath.Join(root, "worktree")
	u.Git(t, repo, "worktree", "add", "-b", "experiment", wt)
	u.Write(t, filepath.Join(wt, "edited.txt"), "changed")
	u.Write(t, filepath.Join(wt, "binary"), "\x00after\xff")
	u.Write(t, filepath.Join(wt, "untracked"), "new")
	u.Write(t, filepath.Join(wt, "ignored"), "skip")
	u.Write(t, filepath.Join(wt, ".env"), "secret")
	u.OK(t, os.Remove(filepath.Join(wt, "deleted.txt")))
	s, err := project.Prepare(t.Context(), host.Exec{}, wt)
	u.OK(t, err)
	defer os.RemoveAll(s.Dir)
	b, err := os.ReadFile(filepath.Join(s.Dir, "edited.txt"))
	u.OK(t, err)
	u.Equal(t, string(b), "changed")
	b, err = os.ReadFile(filepath.Join(s.Dir, "binary"))
	u.OK(t, err)
	u.Equal(t, string(b), "\x00after\xff")
	_, err = os.Stat(filepath.Join(s.Dir, "ignored"))
	u.Equal(t, os.IsNotExist(err), true)
	_, err = os.Stat(filepath.Join(s.Dir, ".env"))
	u.Equal(t, os.IsNotExist(err), true)
	_, err = os.Stat(filepath.Join(s.Dir, "deleted.txt"))
	u.Equal(t, os.IsNotExist(err), true)
	info, err := os.Stat(filepath.Join(s.Dir, ".git"))
	u.OK(t, err)
	u.Equal(t, info.IsDir(), true)
	u.Equal(t, strings.TrimSpace(u.Git(t, s.Dir, "status", "--porcelain")), "")
	u.Equal(t, strings.TrimSpace(u.Git(t, s.Dir, "rev-parse", "HEAD")), s.Baseline)
	b, err = os.ReadFile(filepath.Join(wt, "untracked"))
	u.OK(t, err)
	u.Equal(t, string(b), "new")
}
func TestSnapshotRejectsSymlinksEscapingSource(t *testing.T) {
	dir := t.TempDir()
	u.OK(t, os.Symlink("/etc/passwd", filepath.Join(dir, "outside")))
	_, err := project.Prepare(t.Context(), host.Exec{}, dir)
	u.Error(t, err)
}

func TestSnapshotIgnoresHostGitTemplatesAndConfiguration(t *testing.T) {
	source := t.TempDir()
	u.Write(t, filepath.Join(source, "file.txt"), "safe\n")
	template := t.TempDir()
	marker := filepath.Join(t.TempDir(), "hook-ran")
	u.Write(t, filepath.Join(template, "hooks", "pre-commit"), "#!/bin/sh\ntouch "+host.Quote(marker)+"\n")
	u.OK(t, os.Chmod(filepath.Join(template, "hooks", "pre-commit"), 0700))
	global := filepath.Join(t.TempDir(), "gitconfig")
	u.Write(t, global, "[init]\n templateDir = "+template+"\n")
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_TEMPLATE_DIR", template)
	s, err := project.Prepare(t.Context(), host.Exec{}, source)
	u.OK(t, err)
	defer os.RemoveAll(s.Dir)
	_, err = os.Stat(marker)
	u.Equal(t, os.IsNotExist(err), true)
	_, err = os.Stat(filepath.Join(s.Dir, ".git", "hooks", "pre-commit"))
	u.Equal(t, os.IsNotExist(err), true)
	b, err := os.ReadFile(filepath.Join(s.Dir, "file.txt"))
	u.OK(t, err)
	u.Equal(t, string(b), "safe\n")
}
