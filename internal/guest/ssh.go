package guest

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agent-vm/internal/host"
	"agent-vm/internal/model"
)

//go:embed scripts/control.py
var control string

type SSH struct {
	Runner                   host.Runner
	Data, Identity, User, IP string
	VM                       model.VM
}

func (s SSH) alias() string { return "agent-vm-" + s.VM.ID }
func (s SSH) options(tty bool) []string {
	a := []string{"-F", "/dev/null", "-o", "BatchMode=yes", "-o", "IdentitiesOnly=yes", "-o", "ConnectTimeout=5", "-o", "ServerAliveInterval=15", "-o", "ServerAliveCountMax=3", "-o", "StrictHostKeyChecking=yes", "-o", "UserKnownHostsFile=" + filepath.Join(s.Data, "known_hosts"), "-o", "GlobalKnownHostsFile=/dev/null", "-o", "HostKeyAlias=" + s.alias(), "-i", s.Identity}
	if tty {
		a = append(a, "-tt")
	}
	return append(a, s.User+"@"+s.IP)
}
func (s SSH) Run(ctx context.Context, script string, in io.Reader, out, stderr io.Writer, tty bool) ([]byte, error) {
	if s.Identity == "" {
		return nil, fmt.Errorf("identity_file is required: configure a dedicated SSH key and enroll the guest host key")
	}
	prefix := `export PATH="$HOME/.local/bin:$HOME/.npm-global/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"; `
	a := append(s.options(tty), host.Join([]string{"sh", "-c", prefix + script}))
	return s.Runner.Run(ctx, host.Command{Name: "ssh", Args: a, In: in, Out: out, Err: stderr})
}
func (s SSH) Ready(ctx context.Context) error {
	if s.Identity == "" {
		return fmt.Errorf("configure identity_file before connecting to the guest")
	}
	var last error
	for {
		if _, last = s.Run(ctx, "true", nil, nil, nil, false); last == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("SSH readiness: %w; last attempt: %v; verify key and run agent-vm trust %s", ctx.Err(), last, s.VM.Name)
		case <-time.After(time.Second):
		}
	}
}

func CheckSSH(ctx context.Context, r host.Runner) error {
	var stderr strings.Builder
	b, err := r.Run(ctx, host.Command{Name: "ssh", Args: []string{"-V"}, Err: &stderr})
	if err != nil {
		return err
	}
	if err = host.CheckVersion(string(b)+stderr.String(), "10.3", 10); err != nil {
		return fmt.Errorf("system OpenSSH: %w", err)
	}
	return nil
}
func (s SSH) Control(ctx context.Context, op string, request any, result any) error {
	b, err := json.Marshal(request)
	if err != nil {
		return err
	}
	script := host.Join([]string{"python3", "-c", control, op})
	out, err := s.Run(ctx, script, bytes.NewReader(b), nil, nil, false)
	if err != nil {
		return fmt.Errorf("guest %s failed: %w (run agent-vm shell %s to inspect prerequisites)", op, err, s.VM.Name)
	}
	var response struct {
		Error string `json:"error"`
	}
	if err = json.Unmarshal(out, &response); err != nil {
		return fmt.Errorf("decode guest %s response: %w", op, err)
	}
	if response.Error != "" {
		return fmt.Errorf("guest: %s", response.Error)
	}
	if result != nil {
		return json.Unmarshal(out, result)
	}
	return nil
}
func (s SSH) Attach(ctx context.Context, name string) error {
	_, err := s.Run(ctx, "exec "+host.Join([]string{"tmux", "-L", "agent-vm", "attach-session", "-t", name}), os.Stdin, os.Stdout, os.Stderr, true)
	return err
}
func (s SSH) Shell(ctx context.Context) error {
	_, err := s.Run(ctx, `exec "$SHELL" -l`, os.Stdin, os.Stdout, os.Stderr, true)
	return err
}
func (s SSH) Trust(ctx context.Context, fingerprint string) error {
	if !strings.HasPrefix(fingerprint, "SHA256:") {
		return fmt.Errorf("--fingerprint must be the SHA256 fingerprint verified in the guest console")
	}
	b, err := s.Runner.Run(ctx, host.Command{Name: "ssh-keyscan", Args: []string{"-T", "5", "-t", "ed25519", s.IP}})
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(s.Data, "hostkey-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	f.Close()
	out, err := s.Runner.Run(ctx, host.Command{Name: "ssh-keygen", Args: []string{"-lf", f.Name(), "-E", "sha256"}})
	if err != nil {
		return err
	}
	fields := strings.Fields(string(out))
	if len(fields) < 2 || fields[1] != fingerprint {
		return fmt.Errorf("host-key fingerprint does not match independently verified fingerprint")
	}
	return s.EnrollPublicKey(ctx, string(b), false)
}

// EnrollPublicKey accepts a key obtained through the local Tart channel or
// independently verified by Trust. An identical key is safe on bootstrap retry.
func (s SSH) EnrollPublicKey(ctx context.Context, publicKey string, retry bool) error {
	var key string
	for _, line := range strings.Split(strings.TrimSpace(publicKey), "\n") {
		f := strings.Fields(line)
		for i, field := range f {
			if field == "ssh-ed25519" && i+1 < len(f) {
				decoded, err := base64.StdEncoding.DecodeString(f[i+1])
				if err != nil || len(decoded) != 51 || binary.BigEndian.Uint32(decoded[:4]) != 11 || string(decoded[4:15]) != "ssh-ed25519" || binary.BigEndian.Uint32(decoded[15:19]) != 32 {
					return fmt.Errorf("invalid ed25519 public host key")
				}
				if key != "" {
					return fmt.Errorf("expected exactly one ed25519 public host key")
				}
				key = "ssh-ed25519 " + f[i+1]
			}
		}
	}
	if key == "" {
		return fmt.Errorf("no ed25519 host key returned")
	}
	unlock, err := host.Lock(ctx, filepath.Join(s.Data, "locks", "known-hosts.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	path := filepath.Join(s.Data, "known_hosts")
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	entry := s.alias() + " " + key
	for _, line := range strings.Split(string(existing), "\n") {
		f := strings.Fields(line)
		if len(f) > 0 && f[0] == s.alias() {
			if retry && line == entry {
				return nil
			}
			return fmt.Errorf("VM host key already enrolled; refusing replacement; verify deliberate rotation independently")
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.WriteString(entry + "\n")
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
