package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lothardp/hive/internal/state"
)

// cellNotifGroup represents one cell's notifications, collapsed to the latest.
type cellNotifGroup struct {
	cellName string
	latest   state.Notification // most recent notification for this cell
	count    int                // total notifications in this group
}

// notifRow is one line in the notifications view.
type notifRow struct {
	isHeader bool
	headerText string
	group    *cellNotifGroup
}

// NotifsModel manages the notifications tab.
type NotifsModel struct {
	unreadGroups []cellNotifGroup
	readGroups   []cellNotifGroup
	rows         []notifRow
	cursor       int

	// Confirmation state
	confirming bool

	// Messages
	message string

	// Dependencies
	notifRepo *state.NotificationRepository
}

func NewNotifsModel(notifRepo *state.NotificationRepository) NotifsModel {
	return NotifsModel{
		notifRepo: notifRepo,
	}
}

// Messages

type notifsLoaded struct {
	unreadGroups []cellNotifGroup
	readGroups   []cellNotifGroup
}

type notifMarked struct{ cellName string }
type notifMarkFailed struct {
	cellName string
	err      error
}
type notifsAllMarked struct{ count int }
type notifsCleaned struct{ count int }
type notifsCleanFailed struct{ err error }

// NotifSelected is emitted when the user presses Enter on a notification.
// The parent (dashboard or picker) decides what to do with it.
type NotifSelected struct {
	CellName string
	PaneID   string
}

// groupByCell groups notifications by cell, keeping only the latest per cell.
// Input must be sorted by created_at DESC (which List already returns).
func groupByCell(notifs []state.Notification) []cellNotifGroup {
	seen := make(map[string]int) // cellName → index in result
	var groups []cellNotifGroup
	for _, n := range notifs {
		if idx, ok := seen[n.CellName]; ok {
			groups[idx].count++
		} else {
			seen[n.CellName] = len(groups)
			groups = append(groups, cellNotifGroup{
				cellName: n.CellName,
				latest:   n,
				count:    1,
			})
		}
	}
	return groups
}

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
		return notifsLoaded{
			unreadGroups: groupByCell(unread),
			readGroups:   groupByCell(read),
		}
	}
}

func (m *NotifsModel) buildRows() {
	m.rows = nil

	unreadTotal := 0
	for _, g := range m.unreadGroups {
		unreadTotal += g.count
	}
	readTotal := 0
	for _, g := range m.readGroups {
		readTotal += g.count
	}

	m.rows = append(m.rows, notifRow{isHeader: true, headerText: fmt.Sprintf("Unread (%d)", unreadTotal)})
	for i := range m.unreadGroups {
		m.rows = append(m.rows, notifRow{group: &m.unreadGroups[i]})
	}
	m.rows = append(m.rows, notifRow{isHeader: true, headerText: fmt.Sprintf("Read (%d)", readTotal)})
	for i := range m.readGroups {
		m.rows = append(m.rows, notifRow{group: &m.readGroups[i]})
	}
}

// Update handles messages for the notifications tab.
func (m NotifsModel) Update(msg tea.Msg) (NotifsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case notifsLoaded:
		m.unreadGroups = msg.unreadGroups
		m.readGroups = msg.readGroups
		m.buildRows()
		if m.cursor >= len(m.rows) {
			m.cursor = max(len(m.rows)-1, 0)
		}
		m.skipToNotif(1)
		return m, nil

	case notifMarked:
		m.message = fmt.Sprintf("Marked all from %s as read", msg.cellName)
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
		if g := m.selectedGroup(); g != nil {
			cellName := g.cellName
			paneID := g.latest.SourcePane
			return m, tea.Batch(
				func() tea.Msg {
					ctx := context.Background()
					m.notifRepo.MarkReadByCell(ctx, cellName)
					return notifMarked{cellName}
				},
				func() tea.Msg {
					return NotifSelected{CellName: cellName, PaneID: paneID}
				},
			)
		}
		return m, nil

	case key.Matches(msg, notifKeys.Mark):
		if g := m.selectedGroup(); g != nil && !g.latest.Read {
			cellName := g.cellName
			return m, func() tea.Msg {
				ctx := context.Background()
				_, err := m.notifRepo.MarkReadByCell(ctx, cellName)
				if err != nil {
					return notifMarkFailed{cellName, err}
				}
				return notifMarked{cellName}
			}
		}
		return m, nil

	case key.Matches(msg, notifKeys.MarkAll):
		if len(m.unreadGroups) == 0 {
			return m, nil
		}
		return m, func() tea.Msg {
			ctx := context.Background()
			total := 0
			for _, g := range m.unreadGroups {
				count, _ := m.notifRepo.MarkReadByCell(ctx, g.cellName)
				total += count
			}
			return notifsAllMarked{total}
		}

	case key.Matches(msg, notifKeys.Clean):
		if len(m.readGroups) == 0 {
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

	if len(m.unreadGroups) == 0 && len(m.readGroups) == 0 {
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

		g := r.group
		n := &g.latest
		isRead := n.Read

		// Indicator
		var indicator string
		if isRead {
			indicator = dimStyle.Render("○")
		} else {
			indicator = statusAlive.Render("●")
		}

		// Cell name with count
		cellDisplay := g.cellName
		if g.count > 1 {
			cellDisplay = fmt.Sprintf("%s (%d)", g.cellName, g.count)
		}

		// Age
		age := formatAge(time.Since(n.CreatedAt))

		// Line 1: indicator + cell name (with count) + age
		line1 := fmt.Sprintf("    %s %-35s %s", indicator, cellDisplay, age)
		if selected {
			line1 = selectedStyle.Render(line1)
		} else if isRead {
			line1 = dimStyle.Render(line1)
		}
		b.WriteString(line1 + "\n")

		// Line 2: title + message
		title := n.Title
		if title != "" {
			title += ": "
		}
		msg := title + n.Message
		maxMsgLen := width - 8
		if maxMsgLen < 20 {
			maxMsgLen = 60
		}
		if len(msg) > maxMsgLen {
			msg = msg[:maxMsgLen-3] + "..."
		}
		line2 := "      " + msg
		if isRead {
			line2 = dimStyle.Render(line2)
		}
		b.WriteString(line2 + "\n")
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
// multi-line rendering (headers=2 lines, groups=2 lines each).
func (m NotifsModel) cursorLine() int {
	line := 0
	for i, r := range m.rows {
		if i == m.cursor {
			return line
		}
		if r.isHeader {
			line += 2 // header text + separator
		} else {
			line += 2 // cell line + message line
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
		if len(m.rows) > 1 && m.rows[0].isHeader {
			m.cursor = 1
		}
	}
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor >= 0 && m.cursor < len(m.rows) && m.rows[m.cursor].isHeader {
		for j := m.cursor + 1; j < len(m.rows); j++ {
			if !m.rows[j].isHeader {
				m.cursor = j
				return
			}
		}
		for j := m.cursor - 1; j >= 0; j-- {
			if !m.rows[j].isHeader {
				m.cursor = j
				return
			}
		}
	}
}

func (m NotifsModel) selectedGroup() *cellNotifGroup {
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		return m.rows[m.cursor].group
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
