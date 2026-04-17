package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lothardp/hive/internal/config"
)

// ProjectsModel manages the projects tab.
type ProjectsModel struct {
	projects  []config.DiscoveredProject
	filtered  []config.DiscoveredProject
	cursor    int
	filter    string
	searching bool
	hiveDir   string
	editor    string
	message   string
}

func NewProjectsModel(hiveDir, editor string) ProjectsModel {
	return ProjectsModel{
		hiveDir: hiveDir,
		editor:  editor,
	}
}

// Searching reports whether the tab is in filter-input mode.
func (m ProjectsModel) Searching() bool {
	return m.searching
}

// CursorLine returns the content line index the selected row is rendered at.
func (m ProjectsModel) CursorLine() int {
	headerLines := 2
	if m.searching || m.filter != "" {
		headerLines += 2
	}
	return m.cursor + headerLines
}

// Messages

type projectsLoaded struct {
	projects []config.DiscoveredProject
}

type editorFinished struct{ err error }

func (m ProjectsModel) LoadProjects(globalCfg *config.GlobalConfig) tea.Cmd {
	return func() tea.Msg {
		dirs := globalCfg.ResolveProjectDirs()
		projects, _ := config.DiscoverProjects(dirs)
		return projectsLoaded{projects: projects}
	}
}

// Update handles messages for the projects tab.
func (m ProjectsModel) Update(msg tea.Msg) (ProjectsModel, tea.Cmd) {
	switch msg := msg.(type) {
	case projectsLoaded:
		m.projects = msg.projects
		m.applyFilter()
		return m, nil

	case editorFinished:
		if msg.err != nil {
			m.message = fmt.Sprintf("Editor error: %v", msg.err)
		} else {
			m.message = "Config saved"
		}
		return m, nil

	case tea.KeyMsg:
		return m.updateKeys(msg)
	}
	return m, nil
}

func (m ProjectsModel) updateKeys(msg tea.KeyMsg) (ProjectsModel, tea.Cmd) {
	if m.searching {
		switch msg.Type {
		case tea.KeyEscape:
			m.searching = false
			m.filter = ""
			m.applyFilter()
			return m, nil

		case tea.KeyEnter:
			m.searching = false
			return m.editCurrent()

		case tea.KeyUp, tea.KeyCtrlP:
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil

		case tea.KeyDown, tea.KeyCtrlN:
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
			return m, nil

		case tea.KeyBackspace:
			if len(m.filter) > 0 {
				m.filter = m.filter[:len(m.filter)-1]
				m.applyFilter()
			}
			return m, nil

		case tea.KeyRunes:
			m.filter += string(msg.Runes)
			m.applyFilter()
			return m, nil
		}
		return m, nil
	}

	switch {
	case key.Matches(msg, projKeys.Search):
		m.searching = true
		return m, nil

	case key.Matches(msg, projKeys.ClearFilter):
		if m.filter != "" {
			m.filter = ""
			m.applyFilter()
		}
		return m, nil

	case key.Matches(msg, projKeys.Up):
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil

	case key.Matches(msg, projKeys.Down):
		if m.cursor < len(m.filtered)-1 {
			m.cursor++
		}
		return m, nil

	case key.Matches(msg, projKeys.Edit):
		return m.editCurrent()
	}
	return m, nil
}

func (m ProjectsModel) editCurrent() (ProjectsModel, tea.Cmd) {
	if len(m.filtered) == 0 {
		return m, nil
	}
	proj := m.filtered[m.cursor]
	cfgPath := filepath.Join(m.hiveDir, "config", proj.Name+".yml")

	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		defaultCfg := &config.ProjectConfig{
			RepoPath: proj.Path,
		}
		_ = config.WriteDefaultProject(m.hiveDir, proj.Name, defaultCfg)
	}

	c := exec.Command(m.editor, cfgPath)
	return m, tea.ExecProcess(c, func(err error) tea.Msg {
		return editorFinished{err: err}
	})
}

func (m *ProjectsModel) applyFilter() {
	if m.filter == "" {
		m.filtered = m.projects
	} else {
		f := strings.ToLower(m.filter)
		m.filtered = nil
		for _, p := range m.projects {
			if strings.Contains(strings.ToLower(p.Name), f) {
				m.filtered = append(m.filtered, p)
			}
		}
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = max(len(m.filtered)-1, 0)
	}
}

// View renders the projects tab.
func (m ProjectsModel) View(width int) string {
	var b strings.Builder

	if len(m.projects) == 0 {
		b.WriteString("  No projects found. Configure project_dirs in global config.\n")
		return b.String()
	}

	if m.searching || m.filter != "" {
		cursor := ""
		if m.searching {
			cursor = "█"
		}
		b.WriteString(fmt.Sprintf("  Filter: %s%s\n\n", m.filter, cursor))
	}

	// Header
	b.WriteString(fmt.Sprintf("  %-20s %-45s %s\n", "Project", "Path", "Config"))
	b.WriteString(fmt.Sprintf("  %s\n", strings.Repeat("─", 75)))

	if len(m.filtered) == 0 {
		b.WriteString("  " + dimStyle.Render("No matching projects.") + "\n")
		return b.String()
	}

	for i, p := range m.filtered {
		selected := i == m.cursor

		// Check if project config exists
		cfgPath := filepath.Join(m.hiveDir, "config", p.Name+".yml")
		cfgStatus := dimStyle.Render("✗ no config")
		if _, err := os.Stat(cfgPath); err == nil {
			cfgStatus = successStyle.Render("✓ config")
		}

		// Shorten path for display
		displayPath := p.Path
		if home, err := os.UserHomeDir(); err == nil {
			displayPath = strings.Replace(displayPath, home, "~", 1)
		}

		line := fmt.Sprintf("  %-20s %-45s %s", p.Name, displayPath, cfgStatus)

		if selected {
			line = selectedStyle.Render(line)
		}

		b.WriteString(line)
		b.WriteString("\n")
	}

	return b.String()
}

// Footer returns help text for the projects tab.
func (m ProjectsModel) Footer() string {
	if m.searching {
		return helpStyle.Render("type to filter  enter edit  esc clear")
	}
	if m.message != "" {
		return m.message
	}
	if m.filter != "" {
		return helpStyle.Render("enter edit  / refine  esc clear filter  q quit")
	}
	return helpStyle.Render("enter edit  / search  q quit")
}

// Key bindings

type projKeyMap struct {
	Up          key.Binding
	Down        key.Binding
	Edit        key.Binding
	Search      key.Binding
	ClearFilter key.Binding
}

var projKeys = projKeyMap{
	Up:          key.NewBinding(key.WithKeys("up", "k", "ctrl+p")),
	Down:        key.NewBinding(key.WithKeys("down", "j", "ctrl+n")),
	Edit:        key.NewBinding(key.WithKeys("enter", "e")),
	Search:      key.NewBinding(key.WithKeys("/")),
	ClearFilter: key.NewBinding(key.WithKeys("esc")),
}
