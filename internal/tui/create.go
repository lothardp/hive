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

// createStep tracks the current step in the create flow.
type createStep int

const (
	stepPickProject createStep = iota
	stepEnterName
	stepCreating
	stepDone
)

// CreateModel manages the cell creation overlay.
type CreateModel struct {
	step createStep

	// Project picker
	projects       []config.DiscoveredProject
	projectCursor  int
	projectFilter  string

	// Name input
	nameInput      string
	selectedProject config.DiscoveredProject

	// Progress
	progressLine string
	progressCh   chan string

	// Result
	result  *cell.CreateResult
	err     error
	message string

	// Dependencies
	cellService *cell.Service
}

func NewCreateModel(svc *cell.Service, projects []config.DiscoveredProject) *CreateModel {
	return &CreateModel{
		step:        stepPickProject,
		projects:    projects,
		cellService: svc,
	}
}

// Messages

type cellCreated struct {
	result *cell.CreateResult
}
type cellCreateFailed struct{ err error }
type progressMsg struct{ line string }
type progressDone struct{}

// Update handles input for the create overlay.
func (m *CreateModel) Update(msg tea.Msg) (*CreateModel, tea.Cmd) {
	switch msg := msg.(type) {
	case cellCreated:
		m.result = msg.result
		m.step = stepDone
		m.message = fmt.Sprintf("Cell %q created", msg.result.CellName)
		return m, switchToSession(msg.result.CellName)

	case cellCreateFailed:
		m.err = msg.err
		m.step = stepDone
		m.message = fmt.Sprintf("Failed: %v", msg.err)
		return m, nil

	case progressMsg:
		m.progressLine = msg.line
		return m, m.listenProgress()

	case progressDone:
		return m, nil

	case tea.KeyMsg:
		switch m.step {
		case stepPickProject:
			return m.updateProjectPicker(msg)
		case stepEnterName:
			return m.updateNameInput(msg)
		case stepDone:
			// Any key dismisses
			return nil, nil
		}
	}
	return m, nil
}

func (m *CreateModel) updateProjectPicker(msg tea.KeyMsg) (*CreateModel, tea.Cmd) {
	filtered := m.filteredProjects()

	switch {
	case key.Matches(msg, createKeys.Escape):
		return nil, nil // cancel

	case key.Matches(msg, createKeys.Up):
		if m.projectCursor > 0 {
			m.projectCursor--
		}
		return m, nil

	case key.Matches(msg, createKeys.Down):
		if m.projectCursor < len(filtered)-1 {
			m.projectCursor++
		}
		return m, nil

	case key.Matches(msg, createKeys.Enter):
		if len(filtered) == 0 {
			return m, nil
		}
		m.selectedProject = filtered[m.projectCursor]
		m.step = stepEnterName
		m.nameInput = ""
		return m, nil

	case key.Matches(msg, createKeys.Backspace):
		if len(m.projectFilter) > 0 {
			m.projectFilter = m.projectFilter[:len(m.projectFilter)-1]
			m.projectCursor = 0
		}
		return m, nil

	default:
		if msg.Type == tea.KeyRunes {
			m.projectFilter += string(msg.Runes)
			m.projectCursor = 0
		}
		return m, nil
	}
}

func (m *CreateModel) updateNameInput(msg tea.KeyMsg) (*CreateModel, tea.Cmd) {
	switch {
	case key.Matches(msg, createKeys.Escape):
		// Go back to project picker
		m.step = stepPickProject
		return m, nil

	case msg.Type == tea.KeyEnter:
		name := strings.TrimSpace(m.nameInput)
		if name == "" {
			return m, nil
		}
		m.step = stepCreating
		createCmd, listenCmd := m.startCreate(cell.CreateOpts{
			Project:  m.selectedProject.Name,
			Name:     name,
			RepoPath: m.selectedProject.Path,
		})
		return m, tea.Batch(createCmd, listenCmd)

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

func (m *CreateModel) startCreate(opts cell.CreateOpts) (tea.Cmd, tea.Cmd) {
	m.progressCh = make(chan string, 16)
	ch := m.progressCh

	opts.OnProgress = func(s string) {
		select {
		case ch <- s:
		default:
		}
	}

	createCmd := func() tea.Msg {
		ctx := context.Background()
		defer close(ch)
		result, err := m.cellService.Create(ctx, opts)
		if err != nil {
			return cellCreateFailed{err: err}
		}
		return cellCreated{result: result}
	}

	return createCmd, m.listenProgress()
}

func (m *CreateModel) listenProgress() tea.Cmd {
	ch := m.progressCh
	return func() tea.Msg {
		s, ok := <-ch
		if !ok {
			return progressDone{}
		}
		return progressMsg{line: s}
	}
}

// View renders the create overlay.
func (m *CreateModel) View(width, height int) string {
	var b strings.Builder

	switch m.step {
	case stepPickProject:
		b.WriteString("  " + titleStyle.Render("Create Cell — Pick a project") + "\n\n")

		if m.projectFilter != "" {
			b.WriteString(fmt.Sprintf("  Filter: %s\n\n", m.projectFilter))
		}

		filtered := m.filteredProjects()
		if len(filtered) == 0 {
			b.WriteString("  No matching projects.\n")
		}

		maxShow := height - 8
		if maxShow < 5 {
			maxShow = 5
		}
		start := 0
		if m.projectCursor >= maxShow {
			start = m.projectCursor - maxShow + 1
		}

		for i := start; i < len(filtered) && i < start+maxShow; i++ {
			p := filtered[i]
			line := fmt.Sprintf("  %-25s %s", p.Name, dimStyle.Render(p.Path))
			if i == m.projectCursor {
				line = selectedStyle.Render(line)
			}
			b.WriteString(line + "\n")
		}

		b.WriteString("\n")
		b.WriteString("  " + helpStyle.Render("type to filter  enter select  esc cancel"))

	case stepEnterName:
		b.WriteString("  " + titleStyle.Render("Create Cell — Enter name") + "\n\n")
		b.WriteString(fmt.Sprintf("  Project: %s\n\n", projectStyle.Render(m.selectedProject.Name)))
		b.WriteString(fmt.Sprintf("  Cell name: %s█\n", m.nameInput))
		b.WriteString(fmt.Sprintf("  Session:   %s\n", dimStyle.Render(m.selectedProject.Name+"-"+m.nameInput)))
		b.WriteString("\n")
		b.WriteString("  " + helpStyle.Render("enter create  esc back"))

	case stepCreating:
		line := m.progressLine
		if line == "" {
			line = fmt.Sprintf("Cloning %s...", m.selectedProject.Name)
		}
		b.WriteString("  " + titleStyle.Render("Creating cell...") + "\n\n")
		b.WriteString("  " + progressStyle.Render(line) + "\n")

	case stepDone:
		if m.err != nil {
			b.WriteString("  " + titleStyle.Render("Create failed") + "\n\n")
			b.WriteString("  " + errorStyle.Render(m.err.Error()) + "\n")
			b.WriteString("\n  " + helpStyle.Render("any key to dismiss"))
		} else {
			b.WriteString("  " + titleStyle.Render("Cell created") + "\n\n")
			b.WriteString("  " + successStyle.Render(m.result.CellName) + "\n")
			if m.result.HookLog != "" {
				b.WriteString("  " + dimStyle.Render(m.result.HookLog) + "\n")
			}
		}
	}

	return b.String()
}

func (m *CreateModel) filteredProjects() []config.DiscoveredProject {
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

// Key bindings for create flow

type createKeyMap struct {
	Up        key.Binding
	Down      key.Binding
	Enter     key.Binding
	Escape    key.Binding
	Backspace key.Binding
}

var createKeys = createKeyMap{
	Up:        key.NewBinding(key.WithKeys("up")),
	Down:      key.NewBinding(key.WithKeys("down")),
	Enter:     key.NewBinding(key.WithKeys("enter")),
	Escape:    key.NewBinding(key.WithKeys("esc")),
	Backspace: key.NewBinding(key.WithKeys("backspace")),
}
