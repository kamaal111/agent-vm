package bootstrap

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"agent-vm/internal/host"
	"agent-vm/internal/model"
	u "agent-vm/internal/testutil"
)

func cursorFixture(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "cursor.tar.gz")
	f, err := os.Create(path)
	u.OK(t, err)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	content := []byte("#!/bin/sh\nprintf '%s\\n' '2026.10.22-27dcf5f'\n")
	u.OK(t, tw.WriteHeader(&tar.Header{Name: "dist-package/cursor-agent", Mode: 0700, Size: int64(len(content))}))
	_, err = tw.Write(content)
	u.OK(t, err)
	u.OK(t, tw.Close())
	u.OK(t, gz.Close())
	u.OK(t, f.Close())
	return path
}
func TestCursorArchiveInstallsItsActualLauncherAndRetriesIdempotently(t *testing.T) {
	dir := t.TempDir()
	fixture := cursorFixture(t, dir)
	fakeBin := filepath.Join(dir, "bin")
	u.OK(t, os.Mkdir(fakeBin, 0700))
	for _, tool := range []string{"python3", "git", "tmux", "node", "npm", "cat", "dpkg-query", "brew", "sw_vers"} {
		u.OK(t, os.WriteFile(filepath.Join(fakeBin, tool), []byte("#!/bin/sh\necho fixture-version\n"), 0700))
	}
	body, err := assets.ReadFile("agent.sh")
	u.OK(t, err)
	prefix := "set -eu\nselected=cursor\nversion=2026.10.22-27dcf5f\nplatform=linux\ncursor_sha=unused\nbase=\"$HOME/tools\"\ntmp=\"$HOME/temp\"\nmkdir -p \"$base\" \"$tmp\" \"$HOME/.local/bin\"\nfetch() { cp " + host.Quote(fixture) + " \"$2\"; }\n"
	run := func() error {
		c := exec.Command("sh", "-s")
		c.Env = append(os.Environ(), "HOME="+dir, "PATH="+fakeBin+":"+filepath.Join(dir, ".local", "bin")+":/usr/bin:/bin")
		c.Stdin = strings.NewReader(prefix + string(body))
		out, err := c.CombinedOutput()
		t.Log(string(out))
		return err
	}
	u.OK(t, run())
	link, err := os.Readlink(filepath.Join(dir, ".local", "bin", "agent"))
	u.OK(t, err)
	u.Equal(t, link, filepath.Join(dir, "tools", "cursor-2026.10.22-27dcf5f", "cursor-agent"))
	before, err := os.Stat(filepath.Join(dir, "tools", "cursor-2026.10.22-27dcf5f", "complete"))
	u.OK(t, err)
	u.OK(t, run())
	after, err := os.Stat(filepath.Join(dir, "tools", "cursor-2026.10.22-27dcf5f", "complete"))
	u.OK(t, err)
	u.Equal(t, before.ModTime(), after.ModTime())
}

func TestDownloadChecksumMismatchFailsBeforeInstallation(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	u.OK(t, os.Mkdir(bin, 0700))
	curl := `#!/bin/sh
for output do :; done
printf corrupt > "$output"
`
	u.OK(t, os.WriteFile(filepath.Join(bin, "curl"), []byte(curl), 0700))
	body, err := assets.ReadFile("download.sh")
	u.OK(t, err)
	c := exec.Command("sh", "-s")
	c.Env = append(os.Environ(), "PATH="+bin+":/usr/bin:/bin")
	c.Stdin = strings.NewReader("set -eu\ntmp=" + host.Quote(dir) + "\n" + string(body) + "fetch https://example.invalid/archive \"$tmp/archive\" " + strings.Repeat("0", 64) + "\ntouch \"$tmp/installed\"\n")
	out, err := c.CombinedOutput()
	t.Log(string(out))
	u.Error(t, err)
	_, err = os.Stat(filepath.Join(dir, "installed"))
	u.Equal(t, os.IsNotExist(err), true)
}

func TestAddingAgentReusesPrerequisitesWithoutPackageChanges(t *testing.T) {
	home := t.TempDir()
	pins, err := Pins()
	u.OK(t, err)
	bin := filepath.Join(home, ".local", "bin")
	base := filepath.Join(home, ".local", "share", "agent-vm-tools")
	target := filepath.Join(base, "cursor-"+pins.Agents["cursor"])
	u.OK(t, os.MkdirAll(bin, 0700))
	u.OK(t, os.MkdirAll(target, 0700))
	u.Write(t, filepath.Join(base, "prerequisites-"+pins.Revision+".complete"), "")
	u.Write(t, filepath.Join(target, "complete"), "")
	launcher := "#!/bin/sh\nprintf '%s\\n' " + host.Quote(pins.Agents["cursor"]) + "\n"
	u.OK(t, os.WriteFile(filepath.Join(target, "cursor-agent"), []byte(launcher), 0700))
	binaries := map[string]string{
		"uname":   "case \"$1\" in -s) echo Darwin;; -m) echo arm64;; *) echo fixture-kernel;; esac",
		"node":    "echo v" + pins.Node,
		"npm":     "echo " + pins.NPM,
		"tmux":    "echo 'tmux " + pins.Tmux + "'",
		"python3": "echo fixture-python",
		"git":     "echo fixture-git",
		"sw_vers": "echo 27.0",
		"brew":    "test \"$1\" = list || { touch \"$HOME/packages-changed\"; exit 1; }; echo fixture-inventory",
		"curl":    "touch \"$HOME/downloaded\"; exit 1",
	}
	for name, body := range binaries {
		u.OK(t, os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0700))
	}
	script, err := Tools(model.MacOS, "cursor")
	u.OK(t, err)
	c := exec.Command("sh", "-s")
	c.Env = append(os.Environ(), "HOME="+home)
	c.Stdin = strings.NewReader(script)
	out, err := c.CombinedOutput()
	t.Log(string(out))
	u.OK(t, err)
	_, err = os.Stat(filepath.Join(home, "packages-changed"))
	u.Equal(t, os.IsNotExist(err), true)
	_, err = os.Stat(filepath.Join(home, "downloaded"))
	u.Equal(t, os.IsNotExist(err), true)
}

func TestMacCursorPreservesKeychainAndInstalledFilesForLogin(t *testing.T) {
	home := t.TempDir()
	base := filepath.Join(home, "tools")
	target := filepath.Join(base, "cursor-2026.10.22-27dcf5f")
	u.OK(t, os.MkdirAll(target, 0700))
	u.OK(t, os.MkdirAll(filepath.Join(home, ".local", "bin"), 0700))
	u.Write(t, filepath.Join(target, "complete"), "")
	launcher := []byte("#!/bin/sh\necho 'Error: Your macOS login keychain is locked.' >&2\nexit 1\n")
	u.OK(t, os.WriteFile(filepath.Join(target, "cursor-agent"), launcher, 0700))
	body, err := assets.ReadFile("agent.sh")
	u.OK(t, err)
	prefix := "set -eu\nselected=cursor\nplatform=darwin\nversion=2026.10.22-27dcf5f\nbase=" + host.Quote(base) + "\ntmp=" + host.Quote(home) + "\n"
	c := exec.Command("sh", "-s")
	c.Env = append(os.Environ(), "HOME="+home)
	c.Stdin = strings.NewReader(prefix + string(body))
	out, err := c.CombinedOutput()
	t.Log(string(out))
	u.Error(t, err)
	u.Equal(t, c.ProcessState.ExitCode(), 42)
	info, statErr := os.Stat(filepath.Join(target, "complete"))
	u.OK(t, statErr)
	u.Equal(t, info.Mode().IsRegular(), true)
	link, statErr := os.Readlink(filepath.Join(home, ".local", "bin", "agent"))
	u.OK(t, statErr)
	u.Equal(t, link, filepath.Join(target, "cursor-agent"))
}
