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
	projects []config.DiscoveredProject
	cursor   int
	hiveDir  string
	editor   string
	message  string
}

func NewProjectsModel(hiveDir, editor string) ProjectsModel {
	return ProjectsModel{
		hiveDir: hiveDir,
		editor:  editor,
	}
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
		if m.cursor >= len(m.projects) {
			m.cursor = max(len(m.projects)-1, 0)
		}
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
	switch {
	case key.Matches(msg, projKeys.Up):
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil

	case key.Matches(msg, projKeys.Down):
		if m.cursor < len(m.projects)-1 {
			m.cursor++
		}
		return m, nil

	case key.Matches(msg, projKeys.Edit):
		if len(m.projects) == 0 {
			return m, nil
		}
		proj := m.projects[m.cursor]
		cfgPath := filepath.Join(m.hiveDir, "config", proj.Name+".yml")

		// Create default config if it doesn't exist
		if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
			defaultCfg := &config.ProjectConfig{
				RepoPath: proj.Path,
			}
			_ = config.WriteDefaultProject(m.hiveDir, proj.Name, defaultCfg)
		}

		editor := m.editor
		c := exec.Command(editor, cfgPath)
		return m, tea.ExecProcess(c, func(err error) tea.Msg {
			return editorFinished{err: err}
		})
	}
	return m, nil
}

// View renders the projects tab.
func (m ProjectsModel) View(width int) string {
	var b strings.Builder

	if len(m.projects) == 0 {
		b.WriteString("  No projects found. Configure project_dirs in global config.\n")
		return b.String()
	}

	// Header
	b.WriteString(fmt.Sprintf("  %-20s %-45s %s\n", "Project", "Path", "Config"))
	b.WriteString(fmt.Sprintf("  %s\n", strings.Repeat("─", 75)))

	for i, p := range m.projects {
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
	if m.message != "" {
		return m.message
	}
	return helpStyle.Render("enter edit config  q quit")
}

// Key bindings

type projKeyMap struct {
	Up   key.Binding
	Down key.Binding
	Edit key.Binding
}

var projKeys = projKeyMap{
	Up:   key.NewBinding(key.WithKeys("up", "k")),
	Down: key.NewBinding(key.WithKeys("down", "j")),
	Edit: key.NewBinding(key.WithKeys("enter", "e")),
}
