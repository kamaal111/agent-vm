package host

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type Command struct {
	Name string
	Args []string
	Dir  string
	Env  []string
	In   io.Reader
	Out  io.Writer
	Err  io.Writer
}
type Runner interface {
	Run(context.Context, Command) ([]byte, error)
	Detach(context.Context, Command, string) error
}
type Exec struct{}
type ExitError struct {
	Tool string
	Code int
}

func (e *ExitError) Error() string {
	return fmt.Sprintf("%s exited with status %d (command arguments and output withheld)", e.Tool, e.Code)
}
func ExitCode(err error) int {
	var e *ExitError
	if errors.As(err, &e) {
		return e.Code
	}
	return 1
}
func (Exec) Run(ctx context.Context, s Command) ([]byte, error) {
	c := exec.CommandContext(ctx, s.Name, s.Args...)
	c.Dir = s.Dir
	c.Env = s.Env
	c.Stdin = s.In
	var out bytes.Buffer
	if s.Out != nil {
		c.Stdout = s.Out
	} else {
		c.Stdout = &out
	}
	c.Stderr = s.Err
	err := c.Run()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, &ExitError{Tool: filepath.Base(s.Name), Code: ee.ExitCode()}
		}
		return nil, fmt.Errorf("execute %s: %w", s.Name, err)
	}
	return out.Bytes(), nil
}
func (Exec) Detach(ctx context.Context, s Command, log string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f, err := os.OpenFile(log, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	c := exec.Command(s.Name, s.Args...)
	c.Stdout = f
	c.Stderr = f
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err = c.Start(); err != nil {
		return err
	}
	go func() { _ = c.Wait() }()
	return nil
}

func Quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func Join(args []string) string {
	q := make([]string, len(args))
	for i, s := range args {
		q[i] = Quote(s)
	}
	return strings.Join(q, " ")
}

// Lock uses an OS advisory lock, released on process death. The file is never unlinked.
func Lock(ctx context.Context, path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func CheckVersion(output string, min string, major int) error {
	re := regexp.MustCompile(`\d+\.\d+(?:\.\d+)?`)
	v := re.FindString(output)
	if v == "" {
		return fmt.Errorf("cannot parse tool version; required >=%s, major %d", min, major)
	}
	parse := func(s string) [3]int {
		var r [3]int
		for i, p := range strings.Split(s, ".") {
			if i < 3 {
				r[i], _ = strconv.Atoi(p)
			}
		}
		return r
	}
	a, b := parse(v), parse(min)
	if a[0] != major {
		return fmt.Errorf("unsupported version %s; require major %d >=%s", v, major, min)
	}
	for i := 0; i < 3; i++ {
		if a[i] > b[i] {
			return nil
		}
		if a[i] < b[i] {
			return fmt.Errorf("unsupported version %s; require >=%s", v, min)
		}
	}
	return nil
}
