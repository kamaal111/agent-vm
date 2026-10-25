package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"agent-vm/internal/config"
	"agent-vm/internal/model"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
)

func Interactive() bool {
	if os.Getenv("TERM") == "dumb" {
		return false
	}
	c := exec.Command("sh", "-c", "test -t 0 && test -t 1")
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	return c.Run() == nil
}
func Require() error {
	if !Interactive() {
		return fmt.Errorf("interactive terminal required; supply an explicit session ID, --detach, or --yes as appropriate")
	}
	return nil
}

type picker struct {
	sessions              []model.Session
	input                 textinput.Model
	cursor, width, height int
	selected              string
	canceled              bool
}

func newPicker(ss []model.Session) picker {
	in := textinput.New()
	in.Placeholder = "Type to filter sessions"
	in.Focus()
	return picker{sessions: ss, input: in, width: 80, height: 24}
}
func (m picker) filtered() []model.Session {
	ss := []model.Session{}
	q := strings.ToLower(m.input.Value())
	for _, s := range m.sessions {
		if strings.Contains(strings.ToLower(s.Label()+" "+s.Agent+" "+s.Project+" "+s.VM+" "+string(s.OS)+" "+s.Status), q) {
			ss = append(ss, s)
		}
	}
	return ss
}
func (m picker) Init() tea.Cmd { return textinput.Blink }
func (m picker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = v.Width
		m.height = v.Height
		m.input.SetWidth(max(1, v.Width-5))
	case tea.KeyPressMsg:
		switch v.String() {
		case "esc", "ctrl+c":
			m.canceled = true
			return m, tea.Quit
		case "up":
			m.cursor = max(0, m.cursor-1)
			return m, nil
		case "down":
			m.cursor = min(max(0, len(m.filtered())-1), m.cursor+1)
			return m, nil
		case "enter":
			ss := m.filtered()
			if len(ss) > 0 {
				m.selected = ss[min(m.cursor, len(ss)-1)].ID
				return m, tea.Quit
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.cursor = min(m.cursor, max(0, len(m.filtered())-1))
	return m, cmd
}
func (m picker) View() tea.View {
	style := lipgloss.NewStyle().MaxWidth(max(1, m.width-2))
	var b strings.Builder
	b.WriteString("agent-vm · Sessions (status last observed)\n" + m.input.View() + "\n\n")
	ss := m.filtered()
	if len(ss) == 0 {
		b.WriteString("No matching sessions. Start one with agent-vm run codex.\n")
	}
	rows := max(1, (m.height-7)/2)
	start := max(0, m.cursor-rows+1)
	for i := start; i < min(len(ss), start+rows); i++ {
		s := ss[i]
		mark := "  "
		if i == m.cursor {
			mark = "> "
		}
		b.WriteString(style.Render(fmt.Sprintf("%s%s · %s · %s\n  %s / %s · %s · %s", mark, s.Label(), s.Agent, s.Project, s.VM, s.OS, s.Status, s.LastUsed.Local().Format(time.DateTime))) + "\n")
	}
	b.WriteString("\n↑/↓ move · Enter attach/resume · Esc back · Ctrl-C cancel")
	v := tea.NewView(lipgloss.NewStyle().MaxWidth(max(1, m.width)).MaxHeight(max(1, m.height)).Render(b.String()))
	v.AltScreen = true
	return v
}
func Pick(ctx context.Context, ss []model.Session) (string, error) {
	if err := Require(); err != nil {
		return "", err
	}
	if len(ss) == 0 {
		return "", fmt.Errorf("no sessions; start one with agent-vm run codex")
	}
	m, err := tea.NewProgram(newPicker(ss), tea.WithContext(ctx), tea.WithOutput(os.Stderr)).Run()
	if err != nil {
		return "", err
	}
	return m.(picker).selected, nil
}

type finished struct{ err error }
type progress struct {
	spinner spinner.Model
	title   string
	work    func() error
	cancel  context.CancelFunc
	err     error
}

func (m progress) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, func() tea.Msg { return finished{m.work()} })
}
func (m progress) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case finished:
		m.err = v.err
		return m, tea.Quit
	case tea.KeyPressMsg:
		if v.String() == "ctrl+c" || v.String() == "esc" {
			m.cancel()
		}
	}
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return m, cmd
}
func (m progress) View() tea.View {
	return tea.NewView(m.spinner.View() + " " + m.title + " · Esc cancels")
}
func Progress(ctx context.Context, title string, enabled bool, work func(context.Context) error) error {
	if !enabled || !Interactive() {
		return work(ctx)
	}
	c, cancel := context.WithCancel(ctx)
	defer cancel()
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	m, err := tea.NewProgram(progress{spinner: sp, title: title, work: func() error { return work(c) }, cancel: cancel}, tea.WithOutput(os.Stderr)).Run()
	if err != nil {
		return err
	}
	return m.(progress).err
}
func Confirm(ctx context.Context, title string) error {
	if err := Require(); err != nil {
		return fmt.Errorf("%w; pass --yes to confirm deletion", err)
	}
	ok := false
	form := huh.NewForm(huh.NewGroup(huh.NewConfirm().Title(title).Affirmative("Delete permanently").Negative("Cancel").Value(&ok))).WithProgramOptions(tea.WithOutput(os.Stderr))
	if err := form.RunWithContext(ctx); err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("canceled")
	}
	return nil
}
func Setup(ctx context.Context, c config.Config) (config.Config, error) {
	if err := Require(); err != nil {
		return c, err
	}
	osName := string(c.DefaultOS)
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Title("Default guest OS").Options(huh.NewOption("Linux ARM64", "linux"), huh.NewOption("macOS ARM64", "macos")).Value(&osName),
		huh.NewInput().Title("SSH key override (optional)").Value(&c.IdentityFile),
		huh.NewInput().Title("Linux image override (optional)").Value(&c.Linux.BaseImage),
		huh.NewInput().Title("macOS image override (optional)").Value(&c.MacOS.BaseImage),
	)).WithProgramOptions(tea.WithOutput(os.Stderr))
	if err := form.RunWithContext(ctx); err != nil {
		return c, err
	}
	c.DefaultOS = model.OS(osName)
	return c, c.Validate()
}

func ConfirmSetup(ctx context.Context) error {
	if err := Require(); err != nil {
		return err
	}
	ok := false
	form := huh.NewForm(huh.NewGroup(huh.NewConfirm().Title("Prepare this VM and install the tools listed above?").Affirmative("Set up").Negative("Cancel").Value(&ok))).WithProgramOptions(tea.WithOutput(os.Stderr))
	if err := form.RunWithContext(ctx); err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("setup canceled")
	}
	return nil
}
