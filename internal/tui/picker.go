package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// pickerItem is one row of the /models picker.
type pickerItem struct {
	fullID  string
	display string
	detail  string
	pinned  bool
	auto    bool
}

func (i pickerItem) Title() string {
	if i.auto {
		return "✨ Auto — let Zeuf route (recommended)"
	}
	t := i.display
	if i.pinned {
		t += "  ● pinned"
	}
	return t
}

func (i pickerItem) Description() string { return i.detail }
func (i pickerItem) FilterValue() string { return i.fullID + " " + i.display + " " + i.detail }

type pickerState struct {
	list list.Model
}

// openPicker builds the /models overlay: Auto first, then rows.
func (m *Model) openPicker(models []PickerModel) {
	items := []list.Item{pickerItem{auto: true, fullID: "", display: "Auto", detail: "best free model per task, with fallback"}}
	pinned := ""
	for _, pm := range models {
		if pm.Pinned {
			pinned = pm.FullID
		}
	}
	for _, pm := range models {
		items = append(items, pickerItem{
			fullID: pm.FullID, display: pm.Display,
			detail: pm.Detail, pinned: pm.Pinned && pm.FullID == pinned,
		})
	}
	delegate := list.NewDefaultDelegate()
	delegate.Styles.SelectedTitle = delegate.Styles.SelectedTitle.
		Foreground(lipgloss.Color("81")).BorderLeftForeground(lipgloss.Color("81"))
	delegate.Styles.SelectedDesc = delegate.Styles.SelectedDesc.
		Foreground(lipgloss.Color("241")).BorderLeftForeground(lipgloss.Color("81"))
	delegate.Styles.DimmedTitle = delegate.Styles.DimmedTitle.
		Foreground(lipgloss.Color("241"))
	delegate.Styles.DimmedDesc = delegate.Styles.DimmedDesc.
		Foreground(lipgloss.Color("238"))
	l := list.New(items, delegate, max(40, m.width-8), max(8, m.height-10))
	l.Title = "Models — type to filter · enter pins · esc cancels"
	l.SetFilteringEnabled(true)
	l.SetShowStatusBar(true)
	l.SetShowHelp(false)
	l.KeyMap.Quit.SetKeys() // typing q must never quit the app from here
	l.FilterInput.Prompt = "❯ "
	l.FilterInput.Placeholder = "fuzzy search…"
	l.FilterInput.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
	l.Styles.FilterCursor = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
	m.picker = &pickerState{list: l}
	m.mode = modePicker
}

func (m Model) pickerView() string {
	if m.picker == nil {
		return ""
	}
	return m.picker.list.View()
}

func (m Model) handlePickerKey(key string, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.picker == nil {
		m.mode = modeChat
		return m, nil
	}
	switch key {
	case "esc":
		// opencode-style: esc first leaves search (keeping the picker),
		// then closes it.
		if m.picker.list.FilterState() == list.Filtering || m.picker.list.FilterInput.Value() != "" {
			var cmd tea.Cmd
			m.picker.list, cmd = m.picker.list.Update(msg)
			return m, cmd
		}
		m.picker = nil
		m.mode = modeChat
		return m, nil
	case "enter":
		sel, ok := m.picker.list.SelectedItem().(pickerItem)
		m.picker = nil
		m.mode = modeChat
		if !ok {
			return m, nil
		}
		if m.actions != nil {
			if sel.auto {
				m.actions <- ActionUnpin{}
			} else {
				m.actions <- ActionPin{FullID: sel.fullID}
			}
		}
		m.blocks = append(m.blocks, block{kind: "system", text: pinNote(sel)})
		m.refreshViewport()
		return m, nil
	}
	// opencode-style type-to-filter: the first printable keystroke opens
	// search and feeds it, so arrows keep navigating until you type.
	if msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] != '/' &&
		m.picker.list.FilterState() != list.Filtering {
		var cmd tea.Cmd
		m.picker.list, cmd = m.picker.list.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
		_ = cmd
	}
	var cmd tea.Cmd
	m.picker.list, cmd = m.picker.list.Update(msg)
	return m, cmd
}

func pinNote(sel pickerItem) string {
	if sel.auto {
		return "Routing: automatic."
	}
	return fmt.Sprintf("Pinned: %s", sel.fullID)
}
