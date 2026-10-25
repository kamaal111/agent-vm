package app_test

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-vm/internal/app"
	"agent-vm/internal/config"
	"agent-vm/internal/host"
	"agent-vm/internal/model"
	"agent-vm/internal/project"
	u "agent-vm/internal/testutil"
)

func TestExportPreservesCommittedBinaryDeletionAndUntrackedChangesWithoutApplying(t *testing.T) {
	a, m := fixture(t)
	source := t.TempDir()
	u.Write(t, filepath.Join(source, "text"), "before\n")
	u.Write(t, filepath.Join(source, "binary"), "\x00before\xff")
	u.Write(t, filepath.Join(source, "deleted"), "gone\n")
	snap, err := project.Prepare(t.Context(), host.Exec{}, source)
	u.OK(t, err)
	defer os.RemoveAll(snap.Dir)
	u.Write(t, filepath.Join(snap.Dir, "text"), "after\n")
	u.Write(t, filepath.Join(snap.Dir, "binary"), "\x00after\xff")
	u.OK(t, os.Remove(filepath.Join(snap.Dir, "deleted")))
	u.Git(t, snap.Dir, "add", "-A")
	u.Git(t, snap.Dir, "-c", "user.name=Test", "-c", "user.email=test@example.test", "-c", "commit.gpgsign=false", "commit", "-m", "agent changes")
	u.Write(t, filepath.Join(snap.Dir, "new-file"), "new\n")
	v, err := a.Ensure(t.Context(), app.Selection{})
	u.OK(t, err)
	s := model.Session{ID: model.ID(), Agent: "codex", Created: time.Now(), LastUsed: time.Now(), Status: "exited"}
	w := model.Workspace{ID: model.ID(), VMID: v.ID, Project: "project", Source: source, Path: snap.Dir, Baseline: snap.Baseline}
	u.OK(t, a.Store.AddSession(t.Context(), w, s))
	r := u.Runner{Do: func(ctx context.Context, c host.Command) ([]byte, error) {
		if c.Name == "ssh" && c.Out != nil {
			return (host.Exec{}).Run(ctx, host.Command{Name: "sh", Args: []string{"-c", c.Args[len(c.Args)-1]}, Out: c.Out})
		}
		return m.Run(ctx, c)
	}, Start: m.Detach}
	a.Runner = r
	a.Tart.Runner = r
	output := filepath.Join(t.TempDir(), "export.tar")
	u.OK(t, a.Export(t.Context(), s.ID, output))
	u.Error(t, a.Export(t.Context(), s.ID, output))
	b, err := os.ReadFile(output)
	u.OK(t, err)
	files := readTar(t, b)
	u.Equal(t, strings.Contains(string(files["changes.patch"]), "GIT binary patch"), true)
	u.Equal(t, strings.Contains(string(files["changes.patch"]), "deleted file mode"), true)
	u.Equal(t, strings.Contains(string(files["changes.patch"]), "+after"), true)
	workspace := readTar(t, files["workspace.tar"])
	u.Equal(t, string(workspace["./new-file"]), "new\n")
	u.Equal(t, workspace["./deleted"], []byte(nil))
	b, err = os.ReadFile(filepath.Join(source, "text"))
	u.OK(t, err)
	u.Equal(t, string(b), "before\n")
}
func readTar(t *testing.T, b []byte) map[string][]byte {
	t.Helper()
	r := tar.NewReader(bytes.NewReader(b))
	out := map[string][]byte{}
	for {
		h, err := r.Next()
		if err == io.EOF {
			break
		}
		u.OK(t, err)
		body, err := io.ReadAll(r)
		u.OK(t, err)
		out[h.Name] = body
	}
	return out
}
func TestCodexCredentialImportConflictNeverCopiesBackOrLeaksArguments(t *testing.T) {
	a, m := fixture(t)
	source := filepath.Join(t.TempDir(), "auth.json")
	u.Write(t, source, `{"tokens":{"access_token":"test-secret-never-real"}}`)
	a.Config.Auth["codex"] = config.Auth{Source: "file", File: source}
	v, err := a.Ensure(t.Context(), app.Selection{})
	u.OK(t, err)
	var captured host.Command
	r := u.Runner{Do: func(ctx context.Context, c host.Command) ([]byte, error) {
		if c.Name == "ssh" && strings.Contains(c.Args[len(c.Args)-1], "FileExistsError") {
			captured = c
			return nil, &host.ExitError{Tool: "ssh", Code: 73}
		}
		return m.Run(ctx, c)
	}, Start: m.Detach}
	a.Runner = r
	a.Tart.Runner = r
	err = a.ImportCodex(t.Context(), v.Name)
	u.Error(t, err)
	u.Equal(t, strings.Contains(err.Error(), "already exists"), true)
	u.Equal(t, strings.Contains(strings.Join(captured.Args, " "), "test-secret-never-real"), false)
	b, err := os.ReadFile(source)
	u.OK(t, err)
	var value map[string]any
	u.OK(t, json.Unmarshal(b, &value))
	u.Equal(t, string(b), `{"tokens":{"access_token":"test-secret-never-real"}}`)
}
