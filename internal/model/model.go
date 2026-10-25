package model

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"time"
)

type OS string

const (
	Linux OS = "linux"
	MacOS OS = "macos"
)

func ParseOS(s string) (OS, error) {
	if s != string(Linux) && s != string(MacOS) {
		return "", fmt.Errorf("unsupported OS %q: use linux or macos", s)
	}
	return OS(s), nil
}

func ValidName(s string) error {
	if !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,62}$`).MatchString(s) {
		return fmt.Errorf("invalid name %q: use 1–63 letters, digits, underscores or hyphens", s)
	}
	return nil
}

func ID() string {
	b := make([]byte, 16)
	_, err := rand.Read(b)
	if err != nil {
		panic(err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}

type VM struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	OS        OS        `json:"os"`
	Image     string    `json:"image"`
	Provision string    `json:"provision"`
	State     string    `json:"state"`
	Created   time.Time `json:"created"`
}

type Workspace struct {
	ID       string `json:"id"`
	VMID     string `json:"vm_id"`
	Project  string `json:"project"`
	Source   string `json:"source"`
	Path     string `json:"path"`
	Baseline string `json:"baseline"`
}

type Session struct {
	ID        string    `json:"id"`
	Name      string    `json:"name,omitempty"`
	Agent     string    `json:"agent"`
	NativeID  string    `json:"native_id,omitempty"`
	History   string    `json:"history,omitempty"`
	VMID      string    `json:"vm_id"`
	VM        string    `json:"vm"`
	OS        OS        `json:"os"`
	Workspace string    `json:"workspace"`
	Project   string    `json:"project"`
	Source    string    `json:"source"`
	Baseline  string    `json:"baseline"`
	Created   time.Time `json:"created"`
	LastUsed  time.Time `json:"last_used"`
	Status    string    `json:"status"`
	Args      []string  `json:"-"`
}

func (s Session) Label() string {
	if s.Name != "" {
		return s.Name
	}
	return s.ID
}
func (s Session) Tmux() string { return "avm-" + s.ID }
