package testutil

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"agent-vm/internal/host"
)

func OK(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func Equal(t testing.TB, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v; want %#v", got, want)
	}
}
func Error(t testing.TB, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error")
	}
}
func Write(t testing.TB, path, body string) {
	t.Helper()
	OK(t, os.MkdirAll(filepath.Dir(path), 0700))
	OK(t, os.WriteFile(path, []byte(body), 0600))
}
func Git(t testing.TB, dir string, args ...string) string {
	t.Helper()
	b, err := (host.Exec{}).Run(context.Background(), host.Command{Name: "git", Args: args, Dir: dir})
	OK(t, err)
	return string(b)
}

type Runner struct {
	Do    func(context.Context, host.Command) ([]byte, error)
	Start func(context.Context, host.Command, string) error
}

func (r Runner) Run(ctx context.Context, c host.Command) ([]byte, error) { return r.Do(ctx, c) }
func (r Runner) Detach(ctx context.Context, c host.Command, path string) error {
	return r.Start(ctx, c, path)
}
