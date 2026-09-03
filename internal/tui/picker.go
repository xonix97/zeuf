package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
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
	l := list.New(items, delegate, max(40, m.width-8), max(8, m.height-10))
	l.Title = "Models — enter pins · esc cancels · type to filter"
	l.SetFilteringEnabled(true)
	l.SetShowStatusBar(true)
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
