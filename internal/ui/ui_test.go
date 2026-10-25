package ui

import (
	"testing"

	"agent-vm/internal/model"
	u "agent-vm/internal/testutil"
	tea "charm.land/bubbletea/v2"
)

func TestPickerFiltersAndPreservesStableSessionSelection(t *testing.T) {
	m := newPicker([]model.Session{{ID: "1", Name: "dark-mode", Agent: "codex", VM: "linux"}, {ID: "2", Name: "fix-tests", Agent: "claude", VM: "macos"}})
	m.input.SetValue("claude")
	u.Equal(t, len(m.filtered()), 1)
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	u.Equal(t, updated.(picker).selected, "2")
}
func TestPickerHandlesEmptyResultsNarrowTerminalAndEscape(t *testing.T) {
	m := newPicker(nil)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 5, Height: 3})
	m = updated.(picker)
	u.Equal(t, m.width, 5)
	u.Equal(t, m.View().Content != "", true)
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	u.Equal(t, updated.(picker).canceled, true)
}
