package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lothardp/hive/internal/state"
)

// NotifPickerResult holds what the user selected in the picker.
type NotifPickerResult struct {
	CellName string
	PaneID   string
}

// NotifsPickerModel is a standalone tea.Model that wraps NotifsModel.
// On Enter it stores the selection and quits, letting the caller handle switching.
type NotifsPickerModel struct {
	inner    NotifsModel
	result   *NotifPickerResult
	width    int
	height   int
}

func NewNotifsPickerModel(notifRepo *state.NotificationRepository) NotifsPickerModel {
	return NotifsPickerModel{
		inner: NewNotifsModel(notifRepo),
	}
}

// Result returns the user's selection, or nil if they cancelled.
func (m NotifsPickerModel) Result() *NotifPickerResult {
	return m.result
}

func (m NotifsPickerModel) Init() tea.Cmd {
	return m.inner.LoadNotifs()
}

func (m NotifsPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "q" || msg.Type == tea.KeyCtrlC || msg.Type == tea.KeyEscape {
			return m, tea.Quit
		}

	case NotifSelected:
		m.result = &NotifPickerResult{
			CellName: msg.CellName,
			PaneID:   msg.PaneID,
		}
		return m, tea.Quit
	}

	var cmd tea.Cmd
	m.inner, cmd = m.inner.Update(msg)
	return m, cmd
}

func (m NotifsPickerModel) View() string {
	content := m.inner.View(m.width)
	footer := m.inner.Footer()
	return content + "\n" + footer + "\n"
}
