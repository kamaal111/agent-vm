package project

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"agent-vm/internal/host"
)

type Snapshot struct{ Dir, Source, Project, Baseline string }

func gitEnvironment() []string {
	env := []string{}
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") {
			env = append(env, entry)
		}
	}
	return append(env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_ATTR_NOSYSTEM=1")
}

func Excluded(path string) bool {
	for _, p := range strings.Split(filepath.ToSlash(path), "/") {
		switch p {
		case ".git", "node_modules", "vendor", ".venv", "venv", ".aws", ".ssh", ".codex", ".claude", ".cursor", ".agent-vm", ".DS_Store", ".npmrc", ".netrc", ".pypirc", ".docker", ".kube", "id_rsa", "id_ed25519":
			return true
		}
		if (strings.HasPrefix(p, ".env") && p != ".env.example") || strings.HasSuffix(p, ".pem") || strings.HasSuffix(p, ".key") {
			return true
		}
	}
	return false
}
func Prepare(ctx context.Context, r host.Runner, source string) (Snapshot, error) {
	root, err := filepath.Abs(source)
	if err != nil {
		return Snapshot{}, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return Snapshot{}, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return Snapshot{}, err
	}
	if !info.IsDir() {
		return Snapshot{}, fmt.Errorf("project source is not a directory")
	}
	sum := sha256.Sum256([]byte(root))
	s := Snapshot{Source: root, Project: fmt.Sprintf("%s-%x", filepath.Base(root), sum[:6])}
	s.Dir, err = os.MkdirTemp("", "agent-vm-project-")
	if err != nil {
		return s, err
	}
	ok := false
	defer func() {
		if !ok {
			os.RemoveAll(s.Dir)
		}
	}()
	git := func(args ...string) ([]byte, error) {
		return r.Run(ctx, host.Command{Name: "git", Args: append([]string{"-c", "core.fsmonitor=false"}, args...), Dir: root, Env: gitEnvironment()})
	}
	var files []string
	_, statErr := os.Lstat(filepath.Join(root, ".git"))
	if statErr == nil {
		b, err := git("ls-files", "--stage", "-z")
		if err != nil {
			return s, err
		}
		for _, entry := range strings.Split(string(b), "\x00") {
			if strings.HasPrefix(entry, "160000 ") {
				return s, fmt.Errorf("Git submodules are unsupported; materialize a separate project first")
			}
		}
		b, err = git("ls-files", "--cached", "--others", "--exclude-standard", "-z")
		if err != nil {
			return s, err
		}
		files = strings.Split(string(b), "\x00")
	} else if !os.IsNotExist(statErr) {
		return s, statErr
	} else {
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, e error) error {
			if e != nil {
				return e
			}
			rel, e := filepath.Rel(root, path)
			if e != nil {
				return e
			}
			if rel == "." {
				return nil
			}
			if Excluded(rel) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.IsDir() {
				files = append(files, rel)
			}
			return nil
		})
		if err != nil {
			return s, err
		}
	}
	seen := map[string]bool{}
	for _, rel := range files {
		if err = ctx.Err(); err != nil {
			return s, err
		}
		if rel == "" || Excluded(rel) || seen[rel] {
			continue
		}
		seen[rel] = true
		if !filepath.IsLocal(rel) {
			return s, fmt.Errorf("unsafe project path")
		}
		src := filepath.Join(root, rel)
		info, err := os.Lstat(src)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return s, err
		}
		if info.IsDir() {
			return s, fmt.Errorf("nested repositories/directories in file list unsupported: %s", rel)
		}
		dst := filepath.Join(s.Dir, rel)
		if err = os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
			return s, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(src)
			if err != nil {
				return s, err
			}
			if filepath.IsAbs(target) || !filepath.IsLocal(filepath.Join(filepath.Dir(rel), target)) {
				return s, fmt.Errorf("symlink escapes project: %s", rel)
			}
			if err = os.Symlink(target, dst); err != nil {
				return s, err
			}
			continue
		}
		if !info.Mode().IsRegular() {
			return s, fmt.Errorf("unsupported special file: %s", rel)
		}
		if err = copyFile(src, dst, info.Mode()); err != nil {
			return s, err
		}
	}
	for _, args := range [][]string{{"init", "--initial-branch=main", "--template="}, {"add", "--all", "--force"}, {"-c", "user.name=agent-vm", "-c", "user.email=agent-vm@localhost", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "Host snapshot baseline"}} {
		if _, err = r.Run(ctx, host.Command{Name: "git", Args: args, Dir: s.Dir, Env: gitEnvironment()}); err != nil {
			return s, fmt.Errorf("initialize standalone snapshot: %w", err)
		}
	}
	b, err := r.Run(ctx, host.Command{Name: "git", Args: []string{"rev-parse", "HEAD"}, Dir: s.Dir, Env: gitEnvironment()})
	if err != nil {
		return s, err
	}
	s.Baseline = strings.TrimSpace(string(b))
	ok = true
	return s, nil
}
func copyFile(src, dst string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	perm := fs.FileMode(0600)
	if mode&0111 != 0 {
		perm = 0700
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	e := out.Close()
	if err != nil {
		return err
	}
	return e
}
func (s Snapshot) Archive(ctx context.Context) (*os.File, error) {
	f, err := os.CreateTemp("", "agent-vm-transfer-*.tar")
	if err != nil {
		return nil, err
	}
	tw := tar.NewWriter(f)
	err = filepath.WalkDir(s.Dir, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(s.Dir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		link := ""
		if info.Mode()&os.ModeSymlink != 0 {
			link, err = os.Readlink(path)
			if err != nil {
				return err
			}
		}
		h, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		h.Name = filepath.ToSlash(rel)
		if err = tw.WriteHeader(h); err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			in, err := os.Open(path)
			if err != nil {
				return err
			}
			_, err = io.Copy(tw, in)
			e := in.Close()
			if err != nil {
				return err
			}
			return e
		}
		return nil
	})
	closeErr := tw.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		_, err = f.Seek(0, 0)
	}
	if err != nil {
		f.Close()
		os.Remove(f.Name())
		return nil, err
	}
	return f, nil
}
