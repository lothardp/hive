package tui

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lothardp/hive/internal/state"
	"github.com/lothardp/hive/internal/tmux"
	"github.com/lothardp/hive/internal/worktree"
)

// Styles
var (
	projectStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("4"))
	queenStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	headlessStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	selectedStyle = lipgloss.NewStyle().Background(lipgloss.Color("8")).Bold(true)
	statusAlive   = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	statusDead    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	unreadStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true)
	helpStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	titleStyle    = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	confirmStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
)

// row is one line in the tree view — a project header, a cell, or an unmanaged tmux session.
type row struct {
	isProject   bool
	project     string
	cell        *state.Cell
	tmuxAlive   bool
	unread      int
	tmuxSession string // non-empty for unmanaged tmux sessions (cell is nil)
}

// Model is the Bubble Tea model for the dashboard.
type Model struct {
	rows     []row
	cursor   int
	width    int
	height   int
	quitting bool

	// Dependencies
	cellRepo  *state.CellRepository
	notifRepo *state.NotificationRepository
	tmuxMgr   *tmux.Manager
	wtMgr     *worktree.Manager
	db        *sql.DB
	repoDir   string

	// UI state
	confirming  bool
	confirmName string
	creating    bool
	createInput string
	message     string

	// Output — read by cmd layer after tea.Quit
	SwitchTarget string
}

// NewModel creates a dashboard model with required dependencies.
func NewModel(
	cellRepo *state.CellRepository,
	notifRepo *state.NotificationRepository,
	tmuxMgr *tmux.Manager,
	wtMgr *worktree.Manager,
	db *sql.DB,
	repoDir string,
) Model {
	return Model{
		cellRepo:  cellRepo,
		notifRepo: notifRepo,
		tmuxMgr:   tmuxMgr,
		wtMgr:     wtMgr,
		db:        db,
		repoDir:   repoDir,
	}
}

// Messages

type cellsLoaded struct{ rows []row }
type cellKilled struct{ name string }
type killFailed struct {
	name string
	err  error
}
type clearMsg struct{}

// Init loads cells on startup.
func (m Model) Init() tea.Cmd {
	return m.loadCells
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case cellsLoaded:
		m.rows = msg.rows
		if m.cursor >= len(m.rows) {
			m.cursor = len(m.rows) - 1
		}
		if m.cursor < 0 {
			m.cursor = 0
		}
		m.skipToCell(1)
		return m, nil

	case cellKilled:
		m.message = fmt.Sprintf("Killed %q", msg.name)
		m.confirming = false
		m.confirmName = ""
		return m, tea.Batch(m.loadCells, clearAfter(3*time.Second))

	case killFailed:
		m.message = fmt.Sprintf("Kill %q failed: %v", msg.name, msg.err)
		m.confirming = false
		m.confirmName = ""
		return m, clearAfter(3*time.Second)

	case clearMsg:
		m.message = ""
		return m, nil

	case tea.KeyMsg:
		if m.creating {
			return m.updateCreating(msg)
		}
		if m.confirming {
			return m.updateConfirming(msg)
		}
		return m.updateNormal(msg)
	}

	return m, nil
}

func (m Model) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		m.quitting = true
		return m, tea.Quit

	case key.Matches(msg, keys.Up):
		m.cursor--
		if m.cursor < 0 {
			m.cursor = 0
		}
		m.skipToCell(-1)
		return m, nil

	case key.Matches(msg, keys.Down):
		m.cursor++
		if m.cursor >= len(m.rows) {
			m.cursor = len(m.rows) - 1
		}
		m.skipToCell(1)
		return m, nil

	case key.Matches(msg, keys.Enter):
		if r := m.selectedRow(); r != nil {
			if r.cell != nil {
				m.SwitchTarget = r.cell.Name
				m.quitting = true
				return m, tea.Quit
			}
			if r.tmuxSession != "" {
				m.SwitchTarget = r.tmuxSession
				m.quitting = true
				return m, tea.Quit
			}
		}
		return m, nil

	case key.Matches(msg, keys.Kill):
		if r := m.selectedRow(); r != nil && r.cell != nil {
			m.confirming = true
			m.confirmName = r.cell.Name
		}
		return m, nil

	case key.Matches(msg, keys.Notifs):
		if r := m.selectedRow(); r != nil && r.cell != nil && r.unread > 0 {
			ctx := context.Background()
			count, _ := m.notifRepo.MarkReadByCell(ctx, r.cell.Name)
			m.message = fmt.Sprintf("Marked %d notification(s) read for %s", count, r.cell.Name)
			return m, tea.Batch(m.loadCells, clearAfter(3*time.Second))
		}
		return m, nil

	case key.Matches(msg, keys.Create):
		m.creating = true
		m.createInput = ""
		return m, nil

	case key.Matches(msg, keys.Refresh):
		return m, m.loadCells
	}

	return m, nil
}

func (m Model) updateConfirming(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		return m, m.killCell(m.confirmName)
	default:
		m.confirming = false
		m.confirmName = ""
		return m, nil
	}
}

func (m Model) updateCreating(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		name := strings.TrimSpace(m.createInput)
		m.creating = false
		m.createInput = ""
		if name == "" {
			return m, nil
		}
		m.message = fmt.Sprintf("Run: hive cell %s", name)
		return m, clearAfter(5 * time.Second)
	case tea.KeyEscape:
		m.creating = false
		m.createInput = ""
		return m, nil
	case tea.KeyBackspace:
		if len(m.createInput) > 0 {
			m.createInput = m.createInput[:len(m.createInput)-1]
		}
		return m, nil
	default:
		if msg.Type == tea.KeyRunes {
			m.createInput += string(msg.Runes)
		}
		return m, nil
	}
}

// View renders the dashboard.
func (m Model) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	b.WriteString(titleStyle.Render("Hive Dashboard"))
	b.WriteString("\n\n")

	if len(m.rows) == 0 {
		b.WriteString("  No cells. Press c to create one.\n")
	}

	for i, r := range m.rows {
		selected := i == m.cursor

		if r.isProject {
			line := fmt.Sprintf("▼ %s", r.project)
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
			line := fmt.Sprintf("      %-28s %-20s %s", r.tmuxSession, "", statusAlive.Render("●"))
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
		switch c.Type {
		case state.TypeQueen:
			name = "♛ " + c.Name
		case state.TypeHeadless:
			name = "◇ " + c.Name
		default:
			name = "  " + c.Name
		}

		// Branch
		branch := c.Branch
		if branch == "" {
			branch = "-"
		}

		// Tmux status indicator
		var indicator string
		if r.tmuxAlive {
			indicator = statusAlive.Render("●")
		} else {
			indicator = statusDead.Render("○")
		}

		// Age
		age := formatAge(time.Since(c.CreatedAt))

		line := fmt.Sprintf("    %-30s %-20s %s  %s", name, branch, indicator, age)

		// Unread notifications
		if r.unread > 0 {
			line += "  " + unreadStyle.Render(fmt.Sprintf("(%d unread)", r.unread))
		}

		if selected {
			if c.Type == state.TypeQueen {
				line = selectedStyle.Foreground(lipgloss.Color("3")).Render(line)
			} else {
				line = selectedStyle.Render(line)
			}
		} else {
			switch c.Type {
			case state.TypeQueen:
				line = queenStyle.Render(line)
			case state.TypeHeadless:
				line = headlessStyle.Render(line)
			}
		}

		b.WriteString(line)
		b.WriteString("\n")
	}

	// Footer
	b.WriteString("\n")

	if m.confirming {
		b.WriteString(confirmStyle.Render(fmt.Sprintf("Kill %q? (y/n)", m.confirmName)))
	} else if m.creating {
		b.WriteString(fmt.Sprintf("Cell name: %s█", m.createInput))
	} else if m.message != "" {
		b.WriteString(m.message)
	} else {
		b.WriteString(helpStyle.Render("↑/↓ navigate  enter switch  x kill  n read notifs  c create  r refresh  q quit"))
	}
	b.WriteString("\n")

	return b.String()
}

// Commands

func (m Model) loadCells() tea.Msg {
	ctx := context.Background()
	cells, err := m.cellRepo.List(ctx)
	if err != nil {
		return cellsLoaded{}
	}

	// Group by project
	byProject := make(map[string][]state.Cell)
	var projects []string
	for _, c := range cells {
		p := c.Project
		if p == "" {
			p = "(no project)"
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

func (m Model) killCell(name string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		cell, err := m.cellRepo.GetByName(ctx, name)
		if err != nil || cell == nil {
			return killFailed{name, fmt.Errorf("cell not found")}
		}

		// Queen safety: refuse if other cells exist
		if cell.Type == state.TypeQueen {
			others, err := m.cellRepo.CountByProject(ctx, cell.Project, state.TypeQueen)
			if err != nil || others > 0 {
				return killFailed{name, fmt.Errorf("kill worker cells first")}
			}
		}

		// Kill tmux session
		_ = m.tmuxMgr.KillSession(ctx, name)

		// Normal cells: remove worktree and branch
		if cell.Type == state.TypeNormal && m.repoDir != "" && m.wtMgr != nil {
			_ = m.wtMgr.Remove(ctx, m.repoDir, cell.WorktreePath)
			_ = m.wtMgr.DeleteBranch(ctx, m.repoDir, cell.Branch)
		}

		// Delete DB record
		if err := m.cellRepo.Delete(ctx, name); err != nil {
			return killFailed{name, err}
		}
		return cellKilled{name}
	}
}

func clearAfter(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return clearMsg{} })
}

// Helpers

func (m *Model) skipToCell(dir int) {
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

func (m Model) selectedRow() *row {
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

// Key bindings

type keyMap struct {
	Up      key.Binding
	Down    key.Binding
	Enter   key.Binding
	Kill    key.Binding
	Notifs  key.Binding
	Create  key.Binding
	Refresh key.Binding
	Quit    key.Binding
}

var keys = keyMap{
	Up:      key.NewBinding(key.WithKeys("up", "k")),
	Down:    key.NewBinding(key.WithKeys("down", "j")),
	Enter:   key.NewBinding(key.WithKeys("enter")),
	Kill:    key.NewBinding(key.WithKeys("x")),
	Notifs:  key.NewBinding(key.WithKeys("n")),
	Create:  key.NewBinding(key.WithKeys("c")),
	Refresh: key.NewBinding(key.WithKeys("r")),
	Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c")),
}
