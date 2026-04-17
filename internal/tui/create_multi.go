package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lothardp/hive/internal/cell"
	"github.com/lothardp/hive/internal/config"
)

type multiStep int

const (
	multiStepPickProjects multiStep = iota
	multiStepEnterName
	multiStepCreating
	multiStepDone
)

// CreateMultiModel manages the multicell creation overlay.
type CreateMultiModel struct {
	step multiStep

	// Project multi-picker
	projects      []config.DiscoveredProject
	projectCursor int // index into the virtual list (accept row + filtered projects)
	projectFilter string
	selected      map[string]bool // project name → selected

	// Name input
	nameInput string

	// Result
	result  *cell.MultiResult
	err     error
	message string

	// Dependencies
	cellService *cell.Service
}

func NewCreateMultiModel(svc *cell.Service, projects []config.DiscoveredProject) *CreateMultiModel {
	return &CreateMultiModel{
		step:        multiStepPickProjects,
		projects:    projects,
		selected:    make(map[string]bool),
		cellService: svc,
	}
}

// Messages
type multicellCreated struct {
	result *cell.MultiResult
}
type multicellCreateFailed struct{ err error }

// Update handles input for the multicell overlay.
func (m *CreateMultiModel) Update(msg tea.Msg) (*CreateMultiModel, tea.Cmd) {
	switch msg := msg.(type) {
	case multicellCreated:
		m.result = msg.result
		m.step = multiStepDone
		m.message = fmt.Sprintf("Multicell %q created", msg.result.Name)
		return m, switchToSession(msg.result.Name)

	case multicellCreateFailed:
		m.err = msg.err
		m.step = multiStepDone
		m.message = fmt.Sprintf("Failed: %v", msg.err)
		return m, nil

	case tea.KeyMsg:
		switch m.step {
		case multiStepPickProjects:
			return m.updateProjectPicker(msg)
		case multiStepEnterName:
			return m.updateNameInput(msg)
		case multiStepDone:
			return nil, nil
		}
	}
	return m, nil
}

func (m *CreateMultiModel) updateProjectPicker(msg tea.KeyMsg) (*CreateMultiModel, tea.Cmd) {
	filtered := m.filteredProjects()
	hasAccept := m.selectedCount() > 0
	// Virtual list length = (accept row if any) + filtered projects.
	total := len(filtered)
	if hasAccept {
		total++
	}

	switch {
	case key.Matches(msg, multiKeys.Escape):
		return nil, nil

	case key.Matches(msg, multiKeys.Up):
		if m.projectCursor > 0 {
			m.projectCursor--
		}
		return m, nil

	case key.Matches(msg, multiKeys.Down):
		if m.projectCursor < total-1 {
			m.projectCursor++
		}
		return m, nil

	case key.Matches(msg, multiKeys.Enter):
		// Accept row?
		if hasAccept && m.projectCursor == 0 {
			m.step = multiStepEnterName
			m.nameInput = ""
			return m, nil
		}
		// Project row: toggle selection, clear filter, move cursor to Accept row.
		projIdx := m.projectCursor
		if hasAccept {
			projIdx--
		}
		if projIdx < 0 || projIdx >= len(filtered) {
			return m, nil
		}
		name := filtered[projIdx].Name
		m.selected[name] = !m.selected[name]
		if !m.selected[name] {
			delete(m.selected, name)
		}
		m.projectFilter = ""
		// After toggle, Accept row may have appeared/disappeared.
		if m.selectedCount() > 0 {
			m.projectCursor = 0 // land on Accept row
		} else {
			m.projectCursor = 0
		}
		return m, nil

	case key.Matches(msg, multiKeys.Backspace):
		if len(m.projectFilter) > 0 {
			m.projectFilter = m.projectFilter[:len(m.projectFilter)-1]
			// Keep cursor anchored on Accept row if it's present; else top.
			if m.selectedCount() > 0 {
				m.projectCursor = 0
			} else {
				m.projectCursor = 0
			}
		}
		return m, nil

	default:
		if msg.Type == tea.KeyRunes {
			m.projectFilter += string(msg.Runes)
			// Jump off accept row to the first filtered project.
			if m.selectedCount() > 0 {
				m.projectCursor = 1
			} else {
				m.projectCursor = 0
			}
		}
		return m, nil
	}
}

func (m *CreateMultiModel) updateNameInput(msg tea.KeyMsg) (*CreateMultiModel, tea.Cmd) {
	switch {
	case key.Matches(msg, multiKeys.Escape):
		m.step = multiStepPickProjects
		return m, nil

	case msg.Type == tea.KeyEnter:
		name := strings.TrimSpace(m.nameInput)
		if name == "" {
			return m, nil
		}
		m.step = multiStepCreating
		return m, m.createMulticell(name)

	case msg.Type == tea.KeyBackspace:
		if len(m.nameInput) > 0 {
			m.nameInput = m.nameInput[:len(m.nameInput)-1]
		}
		return m, nil

	default:
		if msg.Type == tea.KeyRunes {
			m.nameInput += string(msg.Runes)
		}
		return m, nil
	}
}

func (m *CreateMultiModel) createMulticell(name string) tea.Cmd {
	selected := m.selectedProjects()
	return func() tea.Msg {
		ctx := context.Background()
		result, err := m.cellService.CreateMulti(ctx, cell.MultiOpts{
			Name:     name,
			Projects: selected,
		})
		if err != nil {
			return multicellCreateFailed{err: err}
		}
		return multicellCreated{result: result}
	}
}

// View renders the multicell overlay.
func (m *CreateMultiModel) View(width, height int) string {
	var b strings.Builder

	switch m.step {
	case multiStepPickProjects:
		b.WriteString("  " + titleStyle.Render("Create Multicell — Pick projects") + "\n\n")
		b.WriteString(fmt.Sprintf("  Filter: %s█", m.projectFilter))
		count := m.selectedCount()
		if count > 0 {
			b.WriteString("    " + dimStyle.Render(fmt.Sprintf("%d selected", count)))
		}
		b.WriteString("\n\n")

		hasAccept := count > 0
		filtered := m.filteredProjects()

		// Render virtual list: [accept?, projects...]
		cursor := m.projectCursor
		idx := 0

		if hasAccept {
			word := "projects"
			if count == 1 {
				word = "project"
			}
			line := fmt.Sprintf("  → Accept (%d %s)", count, word)
			if idx == cursor {
				line = selectedStyle.Render(line)
			} else {
				line = successStyle.Render(line)
			}
			b.WriteString(line + "\n")
			idx++
		}

		if len(filtered) == 0 {
			b.WriteString("  " + dimStyle.Render("No matching projects.") + "\n")
		}

		for _, p := range filtered {
			marker := "  "
			if m.selected[p.Name] {
				marker = "  "
			}
			suffix := ""
			if m.selected[p.Name] {
				suffix = "  " + successStyle.Render("[x]")
			}
			line := fmt.Sprintf("%s%-25s %s%s", marker, p.Name, dimStyle.Render(p.Path), suffix)
			if idx == cursor {
				line = selectedStyle.Render(line)
			}
			b.WriteString(line + "\n")
			idx++
		}

		b.WriteString("\n")
		b.WriteString("  " + helpStyle.Render("type to filter  enter toggle/accept  ↑/↓ move  esc cancel"))

	case multiStepEnterName:
		b.WriteString("  " + titleStyle.Render("Create Multicell — Enter name") + "\n\n")
		b.WriteString(fmt.Sprintf("  Projects: %s\n\n", projectStyle.Render(strings.Join(m.selectedProjectNames(), ", "))))
		b.WriteString(fmt.Sprintf("  Multicell name: %s█\n", m.nameInput))
		b.WriteString("\n")
		b.WriteString("  " + helpStyle.Render("enter create  esc back"))

	case multiStepCreating:
		b.WriteString("  " + titleStyle.Render("Creating multicell...") + "\n\n")
		b.WriteString("  " + progressStyle.Render("Cloning projects and running hooks...") + "\n")

	case multiStepDone:
		if m.err != nil {
			b.WriteString("  " + titleStyle.Render("Multicell create failed") + "\n\n")
			b.WriteString("  " + errorStyle.Render(m.err.Error()) + "\n")
			b.WriteString("\n  " + helpStyle.Render("any key to dismiss"))
		} else {
			b.WriteString("  " + titleStyle.Render("Multicell created") + "\n\n")
			b.WriteString(fmt.Sprintf("  Name:     %s\n", successStyle.Render(m.result.Name)))
			b.WriteString(fmt.Sprintf("  Dir:      %s\n", dimStyle.Render(m.result.ParentDir)))
			b.WriteString(fmt.Sprintf("  Projects: %s\n", strings.Join(m.result.Projects, ", ")))
			b.WriteString(fmt.Sprintf("  Tmux:     %s\n", m.result.Name))
			b.WriteString("\n  " + dimStyle.Render("switching..."))
		}
	}

	return b.String()
}

func (m *CreateMultiModel) filteredProjects() []config.DiscoveredProject {
	if m.projectFilter == "" {
		return m.projects
	}
	filter := strings.ToLower(m.projectFilter)
	var filtered []config.DiscoveredProject
	for _, p := range m.projects {
		if strings.Contains(strings.ToLower(p.Name), filter) {
			filtered = append(filtered, p)
		}
	}
	return filtered
}

func (m *CreateMultiModel) selectedCount() int {
	return len(m.selected)
}

// selectedProjects returns the selected DiscoveredProjects in the order they
// appear in m.projects (stable, alphabetical — matches discovery order).
func (m *CreateMultiModel) selectedProjects() []config.DiscoveredProject {
	var out []config.DiscoveredProject
	for _, p := range m.projects {
		if m.selected[p.Name] {
			out = append(out, p)
		}
	}
	return out
}

func (m *CreateMultiModel) selectedProjectNames() []string {
	var out []string
	for _, p := range m.projects {
		if m.selected[p.Name] {
			out = append(out, p.Name)
		}
	}
	return out
}

// Key bindings for multicell create flow.
type multiKeyMap struct {
	Up        key.Binding
	Down      key.Binding
	Enter     key.Binding
	Escape    key.Binding
	Backspace key.Binding
}

var multiKeys = multiKeyMap{
	Up:        key.NewBinding(key.WithKeys("up", "ctrl+p")),
	Down:      key.NewBinding(key.WithKeys("down", "ctrl+n")),
	Enter:     key.NewBinding(key.WithKeys("enter")),
	Escape:    key.NewBinding(key.WithKeys("esc")),
	Backspace: key.NewBinding(key.WithKeys("backspace")),
}
