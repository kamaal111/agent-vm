package store_test

import (
	"path/filepath"
	"testing"
	"time"

	"agent-vm/internal/model"
	"agent-vm/internal/store"
	u "agent-vm/internal/testutil"
)

func registry(t *testing.T) (*store.Store, model.VM) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "registry.sqlite"))
	u.OK(t, err)
	t.Cleanup(func() { u.OK(t, s.Close()) })
	v := model.VM{ID: model.ID(), Name: "linux", OS: model.Linux, Image: "base-v1", State: "stopped", Created: time.Now().UTC()}
	u.OK(t, s.AddVM(t.Context(), v))
	return s, v
}
func add(t *testing.T, s *store.Store, v model.VM, name string) model.Session {
	t.Helper()
	session := model.Session{ID: model.ID(), Name: name, Agent: "codex", VMID: v.ID, Created: time.Now().UTC(), LastUsed: time.Now().UTC(), Status: "running", Args: []string{"--model", "example"}}
	w := model.Workspace{ID: model.ID(), VMID: v.ID, Path: "/work/" + session.ID, Project: "project", Source: "/source", Baseline: "abc123"}
	u.OK(t, s.AddSession(t.Context(), w, session))
	return session
}
func TestRenamePreservesStableIDAndOldNameStopsResolving(t *testing.T) {
	s, v := registry(t)
	session := add(t, s, v, "dark-mode")
	u.OK(t, s.Rename(t.Context(), "dark-mode", "new-name"))
	got, err := s.Session(t.Context(), session.ID)
	u.OK(t, err)
	u.Equal(t, got.Name, "new-name")
	u.Equal(t, got.Baseline, "abc123")
	u.Equal(t, got.Args, session.Args)
	_, err = s.Session(t.Context(), "dark-mode")
	u.Error(t, err)
}
func TestNamesUniqueAndVMDeletionTransactional(t *testing.T) {
	s, v := registry(t)
	first := add(t, s, v, "unique")
	second := add(t, s, v, "")
	u.Error(t, s.Rename(t.Context(), second.ID, "unique"))
	u.OK(t, s.DeleteVM(t.Context(), v.ID))
	_, err := s.Session(t.Context(), first.ID)
	u.Error(t, err)
	ss, err := s.Sessions(t.Context())
	u.OK(t, err)
	u.Equal(t, len(ss), 0)
}
func TestSessionCreationRollsBackWorkspaceOnNameConflict(t *testing.T) {
	s, v := registry(t)
	add(t, s, v, "unique")
	w := model.Workspace{ID: model.ID(), VMID: v.ID, Path: "/retry"}
	session := model.Session{ID: model.ID(), Name: "unique", Agent: "claude", Created: time.Now(), LastUsed: time.Now()}
	u.Error(t, s.AddSession(t.Context(), w, session))
	session.Name = "retry"
	u.OK(t, s.AddSession(t.Context(), w, session))
	got, err := s.Session(t.Context(), "retry")
	u.OK(t, err)
	u.Equal(t, got.Workspace, "/retry")
}

func TestStableIDWinsOverAnotherSessionsName(t *testing.T) {
	s, v := registry(t)
	first := add(t, s, v, "original")
	second := add(t, s, v, first.ID)
	got, err := s.Session(t.Context(), first.ID)
	u.OK(t, err)
	u.Equal(t, got.ID, first.ID)
	got, err = s.Session(t.Context(), second.ID)
	u.OK(t, err)
	u.Equal(t, got.ID, second.ID)
}
