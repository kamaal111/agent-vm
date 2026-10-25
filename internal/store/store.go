package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"agent-vm/internal/model"
	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	f.Close()
	if err = os.Chmod(path, 0600); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	_, err = db.Exec(`PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000; PRAGMA journal_mode=WAL;
CREATE TABLE IF NOT EXISTS schema_version(version INTEGER PRIMARY KEY);
INSERT OR IGNORE INTO schema_version VALUES(1);
CREATE TABLE IF NOT EXISTS vms(id TEXT PRIMARY KEY,name TEXT NOT NULL UNIQUE,os TEXT NOT NULL CHECK(os IN ('linux','macos')),image TEXT NOT NULL,provision TEXT NOT NULL,state TEXT NOT NULL,created TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS workspaces(id TEXT PRIMARY KEY,vm_id TEXT NOT NULL REFERENCES vms(id),project TEXT NOT NULL,source TEXT NOT NULL,path TEXT NOT NULL,baseline TEXT NOT NULL,UNIQUE(vm_id,path));
CREATE TABLE IF NOT EXISTS sessions(id TEXT PRIMARY KEY,name TEXT UNIQUE,agent TEXT NOT NULL CHECK(agent IN ('codex','claude','cursor')),native_id TEXT NOT NULL DEFAULT '',history TEXT NOT NULL DEFAULT '',workspace_id TEXT NOT NULL REFERENCES workspaces(id),created TEXT NOT NULL,last_used TEXT NOT NULL,status TEXT NOT NULL,args TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS sessions_last_used ON sessions(last_used DESC);`)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize registry: %w", err)
	}
	var version int
	if err = db.QueryRow("SELECT max(version) FROM schema_version").Scan(&version); err != nil || version != 1 {
		db.Close()
		return nil, fmt.Errorf("unsupported registry schema %d: %v", version, err)
	}
	return s, nil
}
func (s *Store) Close() error  { return s.db.Close() }
func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
func (s *Store) AddVM(ctx context.Context, v model.VM) error {
	_, err := s.db.ExecContext(ctx, "INSERT INTO vms VALUES(?,?,?,?,?,?,?)", v.ID, v.Name, v.OS, v.Image, v.Provision, v.State, stamp(v.Created))
	return err
}
func (s *Store) VM(ctx context.Context, name string) (model.VM, error) {
	v := model.VM{}
	var t string
	err := s.db.QueryRowContext(ctx, "SELECT id,name,os,image,provision,state,created FROM vms WHERE name=? OR id=?", name, name).Scan(&v.ID, &v.Name, &v.OS, &v.Image, &v.Provision, &v.State, &t)
	if errors.Is(err, sql.ErrNoRows) {
		return v, fmt.Errorf("VM %q is not owned by agent-vm; create it or explicitly adopt it", name)
	}
	if err == nil {
		v.Created, err = time.Parse(time.RFC3339Nano, t)
	}
	return v, err
}
func (s *Store) VMs(ctx context.Context) ([]model.VM, error) {
	r, err := s.db.QueryContext(ctx, "SELECT id,name,os,image,provision,state,created FROM vms ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer r.Close()
	vs := []model.VM{}
	for r.Next() {
		var v model.VM
		var t string
		if err = r.Scan(&v.ID, &v.Name, &v.OS, &v.Image, &v.Provision, &v.State, &t); err != nil {
			return nil, err
		}
		v.Created, err = time.Parse(time.RFC3339Nano, t)
		if err != nil {
			return nil, err
		}
		vs = append(vs, v)
	}
	return vs, r.Err()
}
func (s *Store) VMState(ctx context.Context, id, state string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE vms SET state=? WHERE id=?", state, id)
	return err
}
func (s *Store) VMProvision(ctx context.Context, id, version string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE vms SET provision=? WHERE id=?", version, id)
	return err
}
func (s *Store) DeleteVM(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{"DELETE FROM sessions WHERE workspace_id IN (SELECT id FROM workspaces WHERE vm_id=?)", "DELETE FROM workspaces WHERE vm_id=?", "DELETE FROM vms WHERE id=?"} {
		if _, err = tx.ExecContext(ctx, q, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) AddSession(ctx context.Context, w model.Workspace, v model.Session) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, "INSERT INTO workspaces VALUES(?,?,?,?,?,?)", w.ID, w.VMID, w.Project, w.Source, w.Path, w.Baseline)
	if err != nil {
		return err
	}
	b, err := json.Marshal(v.Args)
	if err != nil {
		return err
	}
	var name any
	if v.Name != "" {
		name = v.Name
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO sessions VALUES(?,?,?,?,?,?,?,?,?,?)", v.ID, name, v.Agent, v.NativeID, v.History, w.ID, stamp(v.Created), stamp(v.LastUsed), v.Status, string(b))
	if err != nil {
		return fmt.Errorf("record session (names must be unique): %w", err)
	}
	return tx.Commit()
}

const sessionSelect = `SELECT s.id,COALESCE(s.name,''),s.agent,s.native_id,s.history,v.id,v.name,v.os,w.path,w.project,w.source,w.baseline,s.created,s.last_used,s.status,s.args FROM sessions s JOIN workspaces w ON w.id=s.workspace_id JOIN vms v ON v.id=w.vm_id`

type scanner interface{ Scan(...any) error }

func scanSession(r scanner) (model.Session, error) {
	var s model.Session
	var created, used, args string
	err := r.Scan(&s.ID, &s.Name, &s.Agent, &s.NativeID, &s.History, &s.VMID, &s.VM, &s.OS, &s.Workspace, &s.Project, &s.Source, &s.Baseline, &created, &used, &s.Status, &args)
	if err != nil {
		return s, err
	}
	s.Created, err = time.Parse(time.RFC3339Nano, created)
	if err != nil {
		return s, err
	}
	s.LastUsed, err = time.Parse(time.RFC3339Nano, used)
	if err != nil {
		return s, err
	}
	err = json.Unmarshal([]byte(args), &s.Args)
	return s, err
}
func (s *Store) Session(ctx context.Context, id string) (model.Session, error) {
	v, err := scanSession(s.db.QueryRowContext(ctx, sessionSelect+" WHERE s.id=? OR s.name=? ORDER BY CASE WHEN s.id=? THEN 0 ELSE 1 END LIMIT 1", id, id, id))
	if errors.Is(err, sql.ErrNoRows) {
		return v, fmt.Errorf("session %q not found", id)
	}
	return v, err
}
func (s *Store) Sessions(ctx context.Context) ([]model.Session, error) {
	r, err := s.db.QueryContext(ctx, sessionSelect+" ORDER BY s.last_used DESC,s.id")
	if err != nil {
		return nil, err
	}
	defer r.Close()
	ss := []model.Session{}
	for r.Next() {
		v, err := scanSession(r)
		if err != nil {
			return nil, err
		}
		ss = append(ss, v)
	}
	return ss, r.Err()
}
func (s *Store) Rename(ctx context.Context, id, name string) error {
	if err := model.ValidName(name); err != nil {
		return err
	}
	v, err := s.Session(ctx, id)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, "UPDATE sessions SET name=? WHERE id=?", name, v.ID)
	return err
}
func (s *Store) UpdateSession(ctx context.Context, v model.Session) error {
	_, err := s.db.ExecContext(ctx, "UPDATE sessions SET native_id=?,history=?,status=?,last_used=? WHERE id=?", v.NativeID, v.History, v.Status, stamp(v.LastUsed), v.ID)
	return err
}
