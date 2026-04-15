package tui

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lothardp/hive/internal/cell"
	"github.com/lothardp/hive/internal/clone"
	"github.com/lothardp/hive/internal/config"
	"github.com/lothardp/hive/internal/state"
	"github.com/lothardp/hive/internal/tmux"
)

const (
	tabCells    = 0
	tabProjects = 1
	tabConfig   = 2
)

var tabNames = []string{"Cells", "Projects", "Config"}

// Model is the root Bubble Tea model for the dashboard.
type Model struct {
	activeTab int
	cells     CellsModel
	projects  ProjectsModel
	configTab ConfigModel

	// Create flow overlay
	creating      *CreateModel
	// Headless create overlay
	creatingHL    bool
	headlessInput string

	// Dependencies
	cellService *cell.Service
	globalCfg   *config.GlobalConfig
	hiveDir     string

	width, height int
	quitting      bool
}

// NewModel creates a dashboard model with required dependencies.
func NewModel(
	cellRepo *state.CellRepository,
	notifRepo *state.NotificationRepository,
	tmuxMgr *tmux.Manager,
	cloneMgr *clone.Manager,
	globalCfg *config.GlobalConfig,
	hiveDir string,
	db *sql.DB,
) Model {
	svc := &cell.Service{
		CellRepo: cellRepo,
		CloneMgr: cloneMgr,
		TmuxMgr:  tmuxMgr,
		HiveDir:  hiveDir,
		DB:       db,
	}

	editor := globalCfg.ResolveEditor()

	return Model{
		cells:       NewCellsModel(svc, notifRepo, tmuxMgr),
		projects:    NewProjectsModel(hiveDir, editor),
		configTab:   NewConfigModel(hiveDir, editor),
		cellService: svc,
		globalCfg:   globalCfg,
		hiveDir:     hiveDir,
	}
}

// Init loads initial data.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.cells.LoadCells(),
		m.projects.LoadProjects(m.globalCfg),
		m.configTab.LoadConfig(),
	)
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case headlessCreated:
		m.cells.message = fmt.Sprintf("Headless cell %q created", msg.name)
		return m, tea.Batch(m.cells.LoadCells(), switchToSession(msg.name))

	case headlessFailed:
		m.cells.message = fmt.Sprintf("Headless create failed: %v", msg.err)
		return m, nil
	}

	// If creating a cell, delegate to the create overlay
	if m.creating != nil {
		return m.updateCreating(msg)
	}

	// If creating a headless cell, handle that
	if m.creatingHL {
		return m.updateHeadless(msg)
	}

	// Handle tab-level key events first
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch {
		case key.Matches(msg, dashKeys.Quit):
			m.quitting = true
			return m, tea.Quit

		case key.Matches(msg, dashKeys.Tab):
			m.activeTab = (m.activeTab + 1) % len(tabNames)
			return m, nil

		case key.Matches(msg, dashKeys.ShiftTab):
			m.activeTab = (m.activeTab - 1 + len(tabNames)) % len(tabNames)
			return m, nil

		// Create actions (only from cells tab)
		case m.activeTab == tabCells && key.Matches(msg, dashKeys.Create):
			return m.startCreate()

		case m.activeTab == tabCells && key.Matches(msg, dashKeys.Headless):
			m.creatingHL = true
			m.headlessInput = ""
			return m, nil
		}
	}

	// Route data messages to their owning tab regardless of active tab.
	var cmd tea.Cmd
	switch msg.(type) {
	case cellsLoaded, cellKilled, killFailed, cellSwitched, clearMsg:
		m.cells, cmd = m.cells.Update(msg)
		return m, cmd
	case projectsLoaded, editorFinished:
		m.projects, cmd = m.projects.Update(msg)
		if _, ok := msg.(editorFinished); ok {
			m.globalCfg = config.LoadGlobalOrDefault(m.hiveDir)
		}
		return m, cmd
	case configLoaded, configEditorFinished:
		m.configTab, cmd = m.configTab.Update(msg)
		if _, ok := msg.(configEditorFinished); ok {
			m.globalCfg = config.LoadGlobalOrDefault(m.hiveDir)
		}
		return m, cmd
	}

	// Key events go to the active tab.
	switch m.activeTab {
	case tabCells:
		m.cells, cmd = m.cells.Update(msg)
	case tabProjects:
		m.projects, cmd = m.projects.Update(msg)
	case tabConfig:
		m.configTab, cmd = m.configTab.Update(msg)
	}

	return m, cmd
}

func (m Model) startCreate() (tea.Model, tea.Cmd) {
	// Discover projects for the picker
	dirs := m.globalCfg.ResolveProjectDirs()
	projects, _ := config.DiscoverProjects(dirs)
	if len(projects) == 0 {
		m.cells.message = "No projects found. Configure project_dirs first."
		return m, nil
	}
	m.creating = NewCreateModel(m.cellService, projects)
	return m, nil
}

func (m Model) updateCreating(msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, cmd := m.creating.Update(msg)
	if updated == nil {
		// Create flow dismissed
		m.creating = nil
		return m, m.cells.LoadCells()
	}
	m.creating = updated
	return m, cmd
}

func (m Model) updateHeadless(msg tea.Msg) (tea.Model, tea.Cmd) {
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch msg.Type {
		case tea.KeyEscape:
			m.creatingHL = false
			m.headlessInput = ""
			return m, nil

		case tea.KeyEnter:
			name := strings.TrimSpace(m.headlessInput)
			m.creatingHL = false
			m.headlessInput = ""
			if name == "" {
				return m, nil
			}
			return m, m.doCreateHeadless(name)

		case tea.KeyBackspace:
			if len(m.headlessInput) > 0 {
				m.headlessInput = m.headlessInput[:len(m.headlessInput)-1]
			}
			return m, nil

		default:
			if msg.Type == tea.KeyRunes {
				m.headlessInput += string(msg.Runes)
			}
			return m, nil
		}
	}
	return m, nil
}

type headlessCreated struct{ name string }
type headlessFailed struct {
	name string
	err  error
}

func (m Model) doCreateHeadless(name string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		_, err := m.cellService.CreateHeadless(ctx, name)
		if err != nil {
			return headlessFailed{name, err}
		}
		return headlessCreated{name}
	}
}

// View renders the full dashboard.
func (m Model) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	// Title + tabs
	b.WriteString(m.renderHeader())
	b.WriteString("\n\n")

	// Create overlay takes over the content area
	if m.creating != nil {
		b.WriteString(m.creating.View(m.width, m.height))
		b.WriteString("\n")
		return b.String()
	}

	// Headless create prompt
	if m.creatingHL {
		b.WriteString(fmt.Sprintf("  Headless cell name: %s█\n", m.headlessInput))
		b.WriteString("\n  " + helpStyle.Render("enter create  esc cancel") + "\n")
		return b.String()
	}

	// Active tab content
	switch m.activeTab {
	case tabCells:
		b.WriteString(m.cells.View(m.width))
	case tabProjects:
		b.WriteString(m.projects.View(m.width))
	case tabConfig:
		b.WriteString(m.configTab.View(m.width))
	}

	// Footer
	b.WriteString("\n")
	switch m.activeTab {
	case tabCells:
		b.WriteString(m.cells.Footer())
	case tabProjects:
		b.WriteString(m.projects.Footer())
	case tabConfig:
		b.WriteString(m.configTab.Footer())
	}
	b.WriteString("\n")

	return b.String()
}

func (m Model) renderHeader() string {
	title := titleStyle.Render("Hive Dashboard")

	var tabs []string
	for i, name := range tabNames {
		if i == m.activeTab {
			tabs = append(tabs, activeTab.Render(name))
		} else {
			tabs = append(tabs, inactiveTab.Render(name))
		}
	}

	tabBar := strings.Join(tabs, "  ")

	// Put title on left, tabs on right
	gap := m.width - len("Hive Dashboard") - len("Cells  Projects  Config") - 6
	if gap < 2 {
		gap = 2
	}

	return title + strings.Repeat(" ", gap) + tabBar
}

// Key bindings for dashboard-level navigation

type dashKeyMap struct {
	Quit     key.Binding
	Tab      key.Binding
	ShiftTab key.Binding
	Create   key.Binding
	Headless key.Binding
}

var dashKeys = dashKeyMap{
	Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c")),
	Tab:      key.NewBinding(key.WithKeys("tab")),
	ShiftTab: key.NewBinding(key.WithKeys("shift+tab")),
	Create:   key.NewBinding(key.WithKeys("c")),
	Headless: key.NewBinding(key.WithKeys("h")),
}
