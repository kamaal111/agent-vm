// Package bootstrap owns the pinned upstream image and guest installation contract.
package bootstrap

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"

	"agent-vm/internal/host"
	"agent-vm/internal/model"
)

//go:embed manifest.json download.sh tools.sh agent.sh ssh.sh
var assets embed.FS

type Manifest struct {
	Tart struct {
		Version string `json:"version"`
		URL     string `json:"url"`
		SHA256  string `json:"sha256"`
	} `json:"tart"`
	Agents    map[string]string   `json:"agents"`
	Revision  string              `json:"revision"`
	Images    map[model.OS]string `json:"images"`
	Node      string              `json:"node"`
	NPM       string              `json:"npm"`
	Tmux      string              `json:"tmux"`
	TmuxSHA   string              `json:"tmux_sha256"`
	NodeSHA   map[model.OS]string `json:"node_sha256"`
	CursorSHA map[model.OS]string `json:"cursor_sha256"`
}

func Pins() (Manifest, error) {
	var m Manifest
	b, err := assets.ReadFile("manifest.json")
	if err != nil {
		return m, err
	}
	err = json.Unmarshal(b, &m)
	return m, err
}

func Image(os model.OS) string {
	m, err := Pins()
	if err != nil {
		return ""
	}
	return m.Images[os]
}

func Tools(os model.OS, selected string) (string, error) {
	m, err := Pins()
	if err != nil {
		return "", err
	}
	platform := "linux"
	if os == model.MacOS {
		platform = "darwin"
	}
	vars := [][2]string{{"revision", m.Revision}, {"platform", platform}, {"selected", selected}, {"version", m.Agents[selected]}, {"node_version", m.Node}, {"npm_version", m.NPM}, {"tmux_version", m.Tmux}, {"node_sha", m.NodeSHA[os]}, {"tmux_sha", m.TmuxSHA}, {"cursor_sha", m.CursorSHA[os]}}
	var s strings.Builder
	s.WriteString("set -eu\n")
	for _, v := range vars {
		if v[1] == "" {
			return "", fmt.Errorf("missing bootstrap pin %s for %s", v[0], os)
		}
		fmt.Fprintf(&s, "%s=%s\n", v[0], host.Quote(v[1]))
	}
	download, err := assets.ReadFile("download.sh")
	if err != nil {
		return "", err
	}
	b, err := assets.ReadFile("tools.sh")
	if err != nil {
		return "", err
	}
	install, err := assets.ReadFile("agent.sh")
	return s.String() + string(download) + string(b) + string(install), err
}

func SSH(user, publicKey string, os model.OS) (string, error) {
	b, err := assets.ReadFile("ssh.sh")
	return "set -eu\nuser=" + host.Quote(user) + "\npublic_key=" + host.Quote(strings.TrimSpace(publicKey)) + "\nexpected_os=" + host.Quote(string(os)) + "\n" + string(b), err
}
