package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lothardp/hive/internal/state"
	"github.com/lothardp/hive/internal/tmux"
)

// notifRow is one line in the notifications view.
type notifRow struct {
	isHeader     bool
	headerText   string
	notification *state.Notification
}

// NotifsModel manages the notifications tab.
type NotifsModel struct {
	unread []state.Notification
	read   []state.Notification
	rows   []notifRow
	cursor int

	// Confirmation state
	confirming bool

	// Messages
	message string

	// Dependencies
	notifRepo *state.NotificationRepository
	tmuxMgr   *tmux.Manager
}

func NewNotifsModel(notifRepo *state.NotificationRepository, tmuxMgr *tmux.Manager) NotifsModel {
	return NotifsModel{
		notifRepo: notifRepo,
		tmuxMgr:   tmuxMgr,
	}
}

// Messages

type notifsLoaded struct {
	unread []state.Notification
	read   []state.Notification
}

type notifMarked struct{ id int64 }
type notifMarkFailed struct {
	id  int64
	err error
}
type notifsAllMarked struct{ count int }
type notifsCleaned struct{ count int }
type notifsCleanFailed struct{ err error }

func (m NotifsModel) LoadNotifs() tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		unread, _ := m.notifRepo.List(ctx, "", true)
		all, _ := m.notifRepo.List(ctx, "", false)
		var read []state.Notification
		for _, n := range all {
			if n.Read {
				read = append(read, n)
			}
		}
		return notifsLoaded{unread: unread, read: read}
	}
}

func (m *NotifsModel) buildRows() {
	m.rows = nil
	m.rows = append(m.rows, notifRow{isHeader: true, headerText: fmt.Sprintf("Unread (%d)", len(m.unread))})
	for i := range m.unread {
		m.rows = append(m.rows, notifRow{notification: &m.unread[i]})
	}
	m.rows = append(m.rows, notifRow{isHeader: true, headerText: fmt.Sprintf("Read (%d)", len(m.read))})
	for i := range m.read {
		m.rows = append(m.rows, notifRow{notification: &m.read[i]})
	}
}

// Update handles messages for the notifications tab.
func (m NotifsModel) Update(msg tea.Msg) (NotifsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case notifsLoaded:
		m.unread = msg.unread
		m.read = msg.read
		m.buildRows()
		if m.cursor >= len(m.rows) {
			m.cursor = max(len(m.rows)-1, 0)
		}
		m.skipToNotif(1)
		return m, nil

	case notifMarked:
		m.message = "Marked as read"
		return m, tea.Batch(m.LoadNotifs(), clearAfter(3*time.Second))

	case notifMarkFailed:
		m.message = fmt.Sprintf("Mark failed: %v", msg.err)
		return m, clearAfter(3*time.Second)

	case notifsAllMarked:
		m.message = fmt.Sprintf("Marked %d notification(s) as read", msg.count)
		return m, tea.Batch(m.LoadNotifs(), clearAfter(3*time.Second))

	case notifsCleaned:
		m.message = fmt.Sprintf("Deleted %d notification(s)", msg.count)
		m.confirming = false
		return m, tea.Batch(m.LoadNotifs(), clearAfter(3*time.Second))

	case notifsCleanFailed:
		m.message = fmt.Sprintf("Clean up failed: %v", msg.err)
		m.confirming = false
		return m, clearAfter(3*time.Second)

	case cellSwitched:
		return m, nil

	case clearMsg:
		m.message = ""
		return m, nil

	case tea.KeyMsg:
		if m.confirming {
			return m.updateConfirming(msg)
		}
		return m.updateNormal(msg)
	}

	return m, nil
}

func (m NotifsModel) updateNormal(msg tea.KeyMsg) (NotifsModel, tea.Cmd) {
	switch {
	case key.Matches(msg, notifKeys.Up):
		m.cursor--
		if m.cursor < 0 {
			m.cursor = 0
		}
		m.skipToNotif(-1)
		return m, nil

	case key.Matches(msg, notifKeys.Down):
		m.cursor++
		if m.cursor >= len(m.rows) {
			m.cursor = len(m.rows) - 1
		}
		m.skipToNotif(1)
		return m, nil

	case key.Matches(msg, notifKeys.Enter):
		if r := m.selectedNotif(); r != nil {
			return m, switchToPane(r.CellName, r.SourcePane)
		}
		return m, nil

	case key.Matches(msg, notifKeys.Mark):
		if r := m.selectedNotif(); r != nil && !r.Read {
			id := r.ID
			return m, func() tea.Msg {
				ctx := context.Background()
				if err := m.notifRepo.MarkRead(ctx, id); err != nil {
					return notifMarkFailed{id, err}
				}
				return notifMarked{id}
			}
		}
		return m, nil

	case key.Matches(msg, notifKeys.MarkAll):
		if len(m.unread) == 0 {
			return m, nil
		}
		return m, func() tea.Msg {
			ctx := context.Background()
			total := 0
			seen := make(map[string]bool)
			for _, n := range m.unread {
				if seen[n.CellName] {
					continue
				}
				seen[n.CellName] = true
				count, _ := m.notifRepo.MarkReadByCell(ctx, n.CellName)
				total += count
			}
			return notifsAllMarked{total}
		}

	case key.Matches(msg, notifKeys.Clean):
		if len(m.read) == 0 {
			return m, nil
		}
		m.confirming = true
		return m, nil

	case key.Matches(msg, notifKeys.Refresh):
		return m, m.LoadNotifs()
	}

	return m, nil
}

func (m NotifsModel) updateConfirming(msg tea.KeyMsg) (NotifsModel, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		return m, func() tea.Msg {
			ctx := context.Background()
			count, err := m.notifRepo.DeleteRead(ctx)
			if err != nil {
				return notifsCleanFailed{err}
			}
			return notifsCleaned{count}
		}
	default:
		m.confirming = false
		return m, nil
	}
}

// View renders the notifications tab.
func (m NotifsModel) View(width int) string {
	var b strings.Builder

	if len(m.unread) == 0 && len(m.read) == 0 {
		b.WriteString("  No notifications.\n")
		return b.String()
	}

	for i, r := range m.rows {
		selected := i == m.cursor

		if r.isHeader {
			line := fmt.Sprintf("  %s", r.headerText)
			b.WriteString(projectStyle.Render(line))
			b.WriteString("\n")
			b.WriteString(fmt.Sprintf("  %s\n", strings.Repeat("─", 60)))
			continue
		}

		n := r.notification

		// Indicator
		var indicator string
		if n.Read {
			indicator = dimStyle.Render("○")
		} else {
			indicator = statusAlive.Render("●")
		}

		// Title
		title := n.Title
		if title == "" {
			title = "(no title)"
		}

		// Age
		age := formatAge(time.Since(n.CreatedAt))

		// Line 1: indicator + title + cell + age
		line1 := fmt.Sprintf("    %s %-25s %-20s %s", indicator, title, dimStyle.Render(n.CellName), age)
		if selected {
			line1 = selectedStyle.Render(line1)
		} else if n.Read {
			line1 = dimStyle.Render(line1)
		}
		b.WriteString(line1 + "\n")

		// Line 2: message
		msg := n.Message
		maxMsgLen := width - 8
		if maxMsgLen < 20 {
			maxMsgLen = 60
		}
		if len(msg) > maxMsgLen {
			msg = msg[:maxMsgLen-3] + "..."
		}
		line2 := "      " + msg
		if n.Read {
			line2 = dimStyle.Render(line2)
		}
		b.WriteString(line2 + "\n")

		// Line 3: details (optional)
		if n.Details != "" {
			details := n.Details
			if len(details) > maxMsgLen {
				details = details[:maxMsgLen-3] + "..."
			}
			b.WriteString(dimStyle.Render("      "+details) + "\n")
		}
	}

	return b.String()
}

// Footer returns help/status text for the notifications tab.
func (m NotifsModel) Footer() string {
	if m.confirming {
		return confirmStyle.Render("Delete all read notifications? (y/n)")
	}
	if m.message != "" {
		return m.message
	}
	return helpStyle.Render("enter jump  m mark read  M mark all  D clean up  r refresh  h/l tabs  q quit")
}

// cursorLine returns the content line index for the cursor, accounting for
// multi-line rendering (headers=2 lines, notifications=2-3 lines each).
func (m NotifsModel) cursorLine() int {
	line := 0
	for i, r := range m.rows {
		if i == m.cursor {
			return line
		}
		if r.isHeader {
			line += 2 // header text + separator
		} else {
			line += 2 // title + message
			if r.notification != nil && r.notification.Details != "" {
				line++ // details line
			}
		}
	}
	return line
}

func (m *NotifsModel) skipToNotif(dir int) {
	for m.cursor >= 0 && m.cursor < len(m.rows) && m.rows[m.cursor].isHeader {
		m.cursor += dir
	}
	if m.cursor < 0 {
		m.cursor = 0
		// Skip past first header
		if len(m.rows) > 1 && m.rows[0].isHeader {
			m.cursor = 1
		}
	}
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	// If we landed on a header and there's nothing else, stay at 0
	if m.cursor >= 0 && m.cursor < len(m.rows) && m.rows[m.cursor].isHeader {
		// Try forward
		for j := m.cursor + 1; j < len(m.rows); j++ {
			if !m.rows[j].isHeader {
				m.cursor = j
				return
			}
		}
		// Try backward
		for j := m.cursor - 1; j >= 0; j-- {
			if !m.rows[j].isHeader {
				m.cursor = j
				return
			}
		}
	}
}

func (m NotifsModel) selectedNotif() *state.Notification {
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		return m.rows[m.cursor].notification
	}
	return nil
}

// Key bindings for notifications tab

type notifKeyMap struct {
	Up      key.Binding
	Down    key.Binding
	Enter   key.Binding
	Mark    key.Binding
	MarkAll key.Binding
	Clean   key.Binding
	Refresh key.Binding
}

var notifKeys = notifKeyMap{
	Up:      key.NewBinding(key.WithKeys("up", "k")),
	Down:    key.NewBinding(key.WithKeys("down", "j")),
	Enter:   key.NewBinding(key.WithKeys("enter")),
	Mark:    key.NewBinding(key.WithKeys("m")),
	MarkAll: key.NewBinding(key.WithKeys("M")),
	Clean:   key.NewBinding(key.WithKeys("D")),
	Refresh: key.NewBinding(key.WithKeys("r")),
}
