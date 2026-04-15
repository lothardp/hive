package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lothardp/hive/internal/cell"
	"github.com/lothardp/hive/internal/shell"
	"github.com/lothardp/hive/internal/state"
	"github.com/lothardp/hive/internal/tmux"
)

// row is one line in the cell tree view.
type row struct {
	isProject   bool
	project     string
	cell        *state.Cell
	tmuxAlive   bool
	unread      int
	tmuxSession string // non-empty for unmanaged tmux sessions
}

// CellsModel manages the cells tab.
type CellsModel struct {
	rows   []row
	cursor int

	// Dependencies
	cellService *cell.Service
	notifRepo   *state.NotificationRepository
	tmuxMgr     *tmux.Manager

	// Kill confirmation
	confirming  bool
	confirmName string

	// Messages
	message string
}

func NewCellsModel(svc *cell.Service, notifRepo *state.NotificationRepository, tmuxMgr *tmux.Manager) CellsModel {
	return CellsModel{
		cellService: svc,
		notifRepo:   notifRepo,
		tmuxMgr:     tmuxMgr,
	}
}

// Messages

type cellsLoaded struct{ rows []row }
type cellKilled struct{ name string }
type killFailed struct {
	name string
	err  error
}
type cellSwitched struct{}
type clearMsg struct{}

func (m CellsModel) LoadCells() tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		cells, err := m.cellService.List(ctx)
		if err != nil {
			return cellsLoaded{}
		}

		// Group by project
		byProject := make(map[string][]state.Cell)
		var projects []string
		for _, c := range cells {
			p := c.Project
			if p == "" {
				p = "(headless)"
			}
			if _, seen := byProject[p]; !seen {
				projects = append(projects, p)
			}
			byProject[p] = append(byProject[p], c)
		}
		sort.Strings(projects)

		// Track cell names to find unmanaged tmux sessions
		cellNames := make(map[string]bool, len(cells))
		for _, c := range cells {
			cellNames[c.Name] = true
		}
		// The dashboard session itself is managed, don't show it
		cellNames["hive"] = true

		var rows []row
		for _, p := range projects {
			rows = append(rows, row{isProject: true, project: p})
			for i := range byProject[p] {
				c := byProject[p][i]
				alive := false
				if ok, err := m.tmuxMgr.SessionExists(ctx, c.Name); err == nil {
					alive = ok
				}
				unread := 0
				if n, err := m.notifRepo.CountUnread(ctx, c.Name); err == nil {
					unread = n
				}
				rows = append(rows, row{cell: &c, tmuxAlive: alive, unread: unread})
			}
		}

		// Discover unmanaged tmux sessions
		allSessions, _ := m.tmuxMgr.ListSessions(ctx)
		var unmanaged []string
		for _, s := range allSessions {
			if !cellNames[s] {
				unmanaged = append(unmanaged, s)
			}
		}
		if len(unmanaged) > 0 {
			sort.Strings(unmanaged)
			rows = append(rows, row{isProject: true, project: "Other tmux sessions"})
			for _, s := range unmanaged {
				rows = append(rows, row{tmuxSession: s, tmuxAlive: true})
			}
		}

		return cellsLoaded{rows: rows}
	}
}

// Update handles messages for the cells tab.
func (m CellsModel) Update(msg tea.Msg) (CellsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case cellsLoaded:
		m.rows = msg.rows
		if m.cursor >= len(m.rows) {
			m.cursor = max(len(m.rows)-1, 0)
		}
		m.skipToCell(1)
		return m, nil

	case cellKilled:
		m.message = fmt.Sprintf("Killed %q", msg.name)
		m.confirming = false
		m.confirmName = ""
		return m, tea.Batch(m.LoadCells(), clearAfter(3*time.Second))

	case killFailed:
		m.message = fmt.Sprintf("Kill %q failed: %v", msg.name, msg.err)
		m.confirming = false
		m.confirmName = ""
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

func (m CellsModel) updateNormal(msg tea.KeyMsg) (CellsModel, tea.Cmd) {
	switch {
	case key.Matches(msg, cellKeys.Up):
		m.cursor--
		if m.cursor < 0 {
			m.cursor = 0
		}
		m.skipToCell(-1)
		return m, nil

	case key.Matches(msg, cellKeys.Down):
		m.cursor++
		if m.cursor >= len(m.rows) {
			m.cursor = len(m.rows) - 1
		}
		m.skipToCell(1)
		return m, nil

	case key.Matches(msg, cellKeys.Enter):
		if r := m.selectedRow(); r != nil {
			target := ""
			if r.cell != nil {
				target = r.cell.Name
			} else if r.tmuxSession != "" {
				target = r.tmuxSession
			}
			if target != "" {
				return m, switchToSession(target)
			}
		}
		return m, nil

	case key.Matches(msg, cellKeys.Kill):
		if r := m.selectedRow(); r != nil {
			if r.cell != nil {
				m.confirming = true
				m.confirmName = r.cell.Name
			} else if r.tmuxSession != "" {
				m.confirming = true
				m.confirmName = r.tmuxSession
			}
		}
		return m, nil

	case key.Matches(msg, cellKeys.Notifs):
		if r := m.selectedRow(); r != nil && r.cell != nil && r.unread > 0 {
			ctx := context.Background()
			count, _ := m.notifRepo.MarkReadByCell(ctx, r.cell.Name)
			m.message = fmt.Sprintf("Marked %d notification(s) read for %s", count, r.cell.Name)
			return m, tea.Batch(m.LoadCells(), clearAfter(3*time.Second))
		}
		return m, nil

	case key.Matches(msg, cellKeys.Refresh):
		return m, m.LoadCells()
	}

	return m, nil
}

func (m CellsModel) updateConfirming(msg tea.KeyMsg) (CellsModel, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		name := m.confirmName
		return m, func() tea.Msg {
			ctx := context.Background()
			// Try killing as a managed cell first; if not found, kill the raw tmux session.
			if err := m.cellService.Kill(ctx, name); err != nil {
				if killErr := m.tmuxMgr.KillSession(ctx, name); killErr != nil {
					return killFailed{name, killErr}
				}
			}
			return cellKilled{name}
		}
	default:
		m.confirming = false
		m.confirmName = ""
		return m, nil
	}
}

// View renders the cells tab.
func (m CellsModel) View(width int) string {
	var b strings.Builder

	if len(m.rows) == 0 {
		b.WriteString("  No cells. Press 'c' to create one.\n")
	}

	for i, r := range m.rows {
		selected := i == m.cursor

		if r.isProject {
			line := fmt.Sprintf("  %s %s", "▼", r.project)
			if selected {
				b.WriteString(selectedStyle.Render(line))
			} else {
				b.WriteString(projectStyle.Render(line))
			}
			b.WriteString("\n")
			continue
		}

		// Unmanaged tmux session
		if r.tmuxSession != "" {
			line := fmt.Sprintf("      %-28s %s", r.tmuxSession, statusAlive.Render("●"))
			if selected {
				line = selectedStyle.Render(line)
			} else {
				line = headlessStyle.Render(line)
			}
			b.WriteString(line)
			b.WriteString("\n")
			continue
		}

		c := r.cell

		// Name with type indicator
		var name string
		if c.Type == state.TypeHeadless {
			name = "  ◇ " + c.Name
		} else {
			name = "    " + c.Name
		}

		// Tmux status
		var indicator string
		if r.tmuxAlive {
			indicator = statusAlive.Render("●")
		} else {
			indicator = statusDead.Render("○")
		}

		// Age
		age := formatAge(time.Since(c.CreatedAt))

		line := fmt.Sprintf("    %-30s %s  %s", name, indicator, age)

		// Unread notifications
		if r.unread > 0 {
			line += "  " + unreadStyle.Render(fmt.Sprintf("(%d unread)", r.unread))
		}

		if selected {
			line = selectedStyle.Render(line)
		} else if c.Type == state.TypeHeadless {
			line = headlessStyle.Render(line)
		}

		b.WriteString(line)
		b.WriteString("\n")
	}

	return b.String()
}

// Footer returns the help/status text for the cells tab.
func (m CellsModel) Footer() string {
	if m.confirming {
		return confirmStyle.Render(fmt.Sprintf("Kill %q? (y/n)", m.confirmName))
	}
	if m.message != "" {
		return m.message
	}
	return helpStyle.Render("enter switch  c create  o open  H headless  x kill  n read notifs  r refresh  h/l tabs  q quit")
}

// Helpers

func switchToSession(name string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		_, _ = shell.Run(ctx, "tmux", "switch-client", "-t", name)
		return cellSwitched{}
	}
}

func clearAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return clearMsg{} })
}

func (m *CellsModel) skipToCell(dir int) {
	for m.cursor >= 0 && m.cursor < len(m.rows) && m.rows[m.cursor].isProject {
		m.cursor += dir
	}
	if m.cursor < 0 {
		m.cursor = 0
		if len(m.rows) > 1 && m.rows[0].isProject {
			m.cursor = 1
		}
	}
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
}

func (m CellsModel) selectedRow() *row {
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		return &m.rows[m.cursor]
	}
	return nil
}

func formatAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// Key bindings for cells tab (only ones handled inside cells model)

type cellKeyMap struct {
	Up      key.Binding
	Down    key.Binding
	Enter   key.Binding
	Kill    key.Binding
	Notifs  key.Binding
	Refresh key.Binding
}

var cellKeys = cellKeyMap{
	Up:      key.NewBinding(key.WithKeys("up", "k")),
	Down:    key.NewBinding(key.WithKeys("down", "j")),
	Enter:   key.NewBinding(key.WithKeys("enter")),
	Kill:    key.NewBinding(key.WithKeys("x")),
	Notifs:  key.NewBinding(key.WithKeys("n")),
	Refresh: key.NewBinding(key.WithKeys("r")),
}
