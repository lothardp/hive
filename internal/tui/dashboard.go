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
	tabNotifs   = 3
)

var tabNames = []string{"Cells", "Projects", "Config", "Notifs"}

// Model is the root Bubble Tea model for the dashboard.
type Model struct {
	activeTab int
	cells     CellsModel
	projects  ProjectsModel
	configTab ConfigModel
	notifs    NotifsModel

	// Create flow overlay
	creating      *CreateModel
	// Headless create overlay
	creatingHL    bool
	headlessInput string

	// Open project overlay (headless cell in a project dir)
	openingProject    bool
	openProjects      []config.DiscoveredProject
	openProjectCursor int
	openProjectFilter string
	openNameStep      bool
	openSelectedProj  config.DiscoveredProject
	openNameInput     string

	// Dependencies
	cellService *cell.Service
	globalCfg   *config.GlobalConfig
	hiveDir     string

	width, height int
	scrollOffset  int
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
		notifs:      NewNotifsModel(notifRepo, tmuxMgr),
		cellService: svc,
		globalCfg:   globalCfg,
		hiveDir:     hiveDir,
	}
}

// SetInitialTab sets the active tab by name before Init is called.
func (m *Model) SetInitialTab(name string) {
	switch strings.ToLower(name) {
	case "cells":
		m.activeTab = tabCells
	case "projects":
		m.activeTab = tabProjects
	case "config":
		m.activeTab = tabConfig
	case "notifs", "notifications":
		m.activeTab = tabNotifs
	}
}

// Init loads initial data.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.cells.LoadCells(),
		m.projects.LoadProjects(m.globalCfg),
		m.configTab.LoadConfig(),
		m.notifs.LoadNotifs(),
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

	// If opening a project (headless in project dir), handle that
	if m.openingProject {
		return m.updateOpenProject(msg)
	}

	// Handle tab-level key events first
	if msg, ok := msg.(tea.KeyMsg); ok {
		switch {
		case key.Matches(msg, dashKeys.Quit):
			m.quitting = true
			return m, tea.Quit

		case key.Matches(msg, dashKeys.Tab), key.Matches(msg, dashKeys.NextTab):
			m.activeTab = (m.activeTab + 1) % len(tabNames)
			m.scrollOffset = 0
			return m, nil

		case key.Matches(msg, dashKeys.ShiftTab), key.Matches(msg, dashKeys.PrevTab):
			m.activeTab = (m.activeTab - 1 + len(tabNames)) % len(tabNames)
			m.scrollOffset = 0
			return m, nil

		// Create actions (only from cells tab)
		case m.activeTab == tabCells && key.Matches(msg, dashKeys.Create):
			return m.startCreate()

		case m.activeTab == tabCells && key.Matches(msg, dashKeys.Headless):
			m.creatingHL = true
			m.headlessInput = ""
			return m, nil

		case m.activeTab == tabCells && key.Matches(msg, dashKeys.OpenProject):
			dirs := m.globalCfg.ResolveProjectDirs()
			projects, _ := config.DiscoverProjects(dirs)
			if len(projects) == 0 {
				m.cells.message = "No projects found. Configure project_dirs first."
				return m, nil
			}
			m.openingProject = true
			m.openProjects = projects
			m.openProjectCursor = 0
			m.openProjectFilter = ""
			m.openNameStep = false
			m.openNameInput = ""
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
	case notifsLoaded, notifMarked, notifMarkFailed, notifsAllMarked, notifsCleaned, notifsCleanFailed:
		m.notifs, cmd = m.notifs.Update(msg)
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
	case tabNotifs:
		m.notifs, cmd = m.notifs.Update(msg)
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
			return m, m.doCreateHeadless(name, "", "")

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

func (m Model) updateOpenProject(msg tea.Msg) (tea.Model, tea.Cmd) {
	kmsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	if m.openNameStep {
		return m.updateOpenProjectName(kmsg)
	}
	return m.updateOpenProjectPicker(kmsg)
}

func (m Model) updateOpenProjectPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	filtered := m.filteredOpenProjects()

	switch {
	case msg.Type == tea.KeyEscape:
		m.openingProject = false
		return m, nil

	case key.Matches(msg, createKeys.Up):
		if m.openProjectCursor > 0 {
			m.openProjectCursor--
		}
		return m, nil

	case key.Matches(msg, createKeys.Down):
		if m.openProjectCursor < len(filtered)-1 {
			m.openProjectCursor++
		}
		return m, nil

	case msg.Type == tea.KeyEnter:
		if len(filtered) == 0 {
			return m, nil
		}
		m.openSelectedProj = filtered[m.openProjectCursor]
		m.openNameStep = true
		m.openNameInput = ""
		return m, nil

	case msg.Type == tea.KeyBackspace:
		if len(m.openProjectFilter) > 0 {
			m.openProjectFilter = m.openProjectFilter[:len(m.openProjectFilter)-1]
			m.openProjectCursor = 0
		}
		return m, nil

	default:
		if msg.Type == tea.KeyRunes {
			m.openProjectFilter += string(msg.Runes)
			m.openProjectCursor = 0
		}
		return m, nil
	}
}

func (m Model) updateOpenProjectName(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyEscape:
		m.openNameStep = false
		return m, nil

	case msg.Type == tea.KeyEnter:
		name := strings.TrimSpace(m.openNameInput)
		m.openingProject = false
		if name == "" {
			return m, nil
		}
		cellName := m.openSelectedProj.Name + "-" + name
		return m, m.doCreateHeadless(cellName, m.openSelectedProj.Name, m.openSelectedProj.Path)

	case msg.Type == tea.KeyBackspace:
		if len(m.openNameInput) > 0 {
			m.openNameInput = m.openNameInput[:len(m.openNameInput)-1]
		}
		return m, nil

	default:
		if msg.Type == tea.KeyRunes {
			m.openNameInput += string(msg.Runes)
		}
		return m, nil
	}
}

func (m Model) filteredOpenProjects() []config.DiscoveredProject {
	if m.openProjectFilter == "" {
		return m.openProjects
	}
	filter := strings.ToLower(m.openProjectFilter)
	var filtered []config.DiscoveredProject
	for _, p := range m.openProjects {
		if strings.Contains(strings.ToLower(p.Name), filter) {
			filtered = append(filtered, p)
		}
	}
	return filtered
}

func (m Model) viewOpenProject() string {
	var b strings.Builder

	if m.openNameStep {
		b.WriteString("  " + titleStyle.Render("Open Project — Enter name") + "\n\n")
		b.WriteString(fmt.Sprintf("  Project: %s\n\n", projectStyle.Render(m.openSelectedProj.Name)))
		b.WriteString(fmt.Sprintf("  Cell name: %s█\n", m.openSelectedProj.Name+"-"+m.openNameInput))
		b.WriteString("\n")
		b.WriteString("  " + helpStyle.Render("enter create  esc back"))
		return b.String()
	}

	b.WriteString("  " + titleStyle.Render("Open Project — Pick a project") + "\n\n")

	if m.openProjectFilter != "" {
		b.WriteString(fmt.Sprintf("  Filter: %s\n\n", m.openProjectFilter))
	}

	filtered := m.filteredOpenProjects()
	if len(filtered) == 0 {
		b.WriteString("  No matching projects.\n")
	}

	for i, p := range filtered {
		line := fmt.Sprintf("  %-25s %s", p.Name, dimStyle.Render(p.Path))
		if i == m.openProjectCursor {
			line = selectedStyle.Render(line)
		}
		b.WriteString(line + "\n")
	}

	b.WriteString("\n")
	b.WriteString("  " + helpStyle.Render("type to filter  enter select  esc cancel"))
	return b.String()
}

type headlessCreated struct{ name string }
type headlessFailed struct {
	name string
	err  error
}

func (m Model) doCreateHeadless(name, project, dir string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		opts := cell.HeadlessOpts{Name: name, Project: project, Dir: dir}
		_, err := m.cellService.CreateHeadless(ctx, opts)
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

	// Header: title + tabs + blank line = 2 lines
	header := m.renderHeader() + "\n\n"
	headerLines := 2

	// Footer: blank line + help text + trailing newline = 3 lines
	var footer string
	switch m.activeTab {
	case tabCells:
		footer = m.cells.Footer()
	case tabProjects:
		footer = m.projects.Footer()
	case tabConfig:
		footer = m.configTab.Footer()
	case tabNotifs:
		footer = m.notifs.Footer()
	}
	footer = "\n" + footer + "\n"
	footerLines := 3

	// Content area height
	contentHeight := m.height - headerLines - footerLines
	if contentHeight < 1 {
		contentHeight = 1
	}

	// Build content
	var content string
	if m.creating != nil {
		content = m.creating.View(m.width, m.height)
	} else if m.openingProject {
		content = m.viewOpenProject()
	} else if m.creatingHL {
		content = fmt.Sprintf("  Headless cell name: %s█\n", m.headlessInput)
		content += "\n  " + helpStyle.Render("enter create  esc cancel") + "\n"
	} else {
		switch m.activeTab {
		case tabCells:
			content = m.cells.View(m.width)
		case tabProjects:
			content = m.projects.View(m.width)
		case tabConfig:
			content = m.configTab.View(m.width)
		case tabNotifs:
			content = m.notifs.View(m.width)
		}
	}

	// Split content into lines and apply scrolling
	lines := strings.Split(content, "\n")
	// Remove trailing empty line from split (content usually ends with \n)
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	// Determine which line the cursor is on for scroll-follow
	cursorLine := m.cursorContentLine()

	// Calculate scroll offset to keep cursor visible
	if cursorLine >= 0 {
		if cursorLine < m.scrollOffset {
			m.scrollOffset = cursorLine
		}
		if cursorLine >= m.scrollOffset+contentHeight {
			m.scrollOffset = cursorLine - contentHeight + 1
		}
	}
	// Clamp
	maxOffset := len(lines) - contentHeight
	if maxOffset < 0 {
		maxOffset = 0
	}
	if m.scrollOffset > maxOffset {
		m.scrollOffset = maxOffset
	}
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}

	// Slice visible lines
	end := m.scrollOffset + contentHeight
	if end > len(lines) {
		end = len(lines)
	}
	visible := lines[m.scrollOffset:end]

	// Pad to fill content area so footer stays at bottom
	for len(visible) < contentHeight {
		visible = append(visible, "")
	}

	return header + strings.Join(visible, "\n") + "\n" + footer
}

// cursorContentLine returns the content line index the cursor is on, or -1.
func (m Model) cursorContentLine() int {
	switch m.activeTab {
	case tabCells:
		return m.cells.cursor
	case tabProjects:
		// +2 for the table header and separator lines
		return m.projects.cursor + 2
	case tabNotifs:
		return m.notifs.cursorLine()
	default:
		return -1
	}
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
	rawTabText := strings.Join(tabNames, "  ")
	gap := m.width - len("Hive Dashboard") - len(rawTabText) - 4
	if gap < 2 {
		gap = 2
	}

	return title + strings.Repeat(" ", gap) + tabBar
}

// Key bindings for dashboard-level navigation

type dashKeyMap struct {
	Quit        key.Binding
	Tab         key.Binding
	ShiftTab    key.Binding
	NextTab     key.Binding
	PrevTab     key.Binding
	Create      key.Binding
	Headless    key.Binding
	OpenProject key.Binding
}

var dashKeys = dashKeyMap{
	Quit:        key.NewBinding(key.WithKeys("q", "ctrl+c")),
	Tab:         key.NewBinding(key.WithKeys("tab")),
	ShiftTab:    key.NewBinding(key.WithKeys("shift+tab")),
	NextTab:     key.NewBinding(key.WithKeys("l")),
	PrevTab:     key.NewBinding(key.WithKeys("h")),
	Create:      key.NewBinding(key.WithKeys("c")),
	Headless:    key.NewBinding(key.WithKeys("H")),
	OpenProject: key.NewBinding(key.WithKeys("o")),
}
