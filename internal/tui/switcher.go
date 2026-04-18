package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lothardp/hive/internal/state"
)

// SwitchItem represents a cell with its tmux liveness status.
type SwitchItem struct {
	Cell      state.Cell
	TmuxAlive bool
}

// SwitcherModel is the Bubble Tea model for the fuzzy cell switcher.
type SwitcherModel struct {
	items    []SwitchItem
	filtered []SwitchItem
	cursor   int
	filter   string
	selected string
	width    int
	height   int
}

// NewSwitcherModel creates a switcher with the given items.
func NewSwitcherModel(items []SwitchItem) SwitcherModel {
	m := SwitcherModel{items: items}
	m.applyFilter()
	return m
}

// Selected returns the cell name the user picked, or "" if cancelled.
func (m SwitcherModel) Selected() string {
	return m.selected
}

func (m SwitcherModel) Init() tea.Cmd {
	return nil
}

func (m SwitcherModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEscape, tea.KeyCtrlC:
			return m, tea.Quit

		case tea.KeyEnter:
			if len(m.filtered) > 0 && m.cursor < len(m.filtered) {
				m.selected = m.filtered[m.cursor].Cell.Name
			}
			return m, tea.Quit

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
	}
	return m, nil
}

func (m SwitcherModel) View() string {
	var b strings.Builder

	if len(m.filtered) == 0 && m.filter == "" {
		b.WriteString("  No cells.\n")
		return b.String()
	}

	// Determine how many items we can show
	// Reserve 3 lines: blank + filter + help
	maxVisible := m.height - 3
	if maxVisible < 1 {
		maxVisible = 20
	}

	// Calculate scroll window around cursor
	start := 0
	if m.cursor >= maxVisible {
		start = m.cursor - maxVisible + 1
	}
	end := start + maxVisible
	if end > len(m.filtered) {
		end = len(m.filtered)
	}

	for i := start; i < end; i++ {
		item := m.filtered[i]
		c := item.Cell

		// Cursor
		prefix := "  "
		if i == m.cursor {
			prefix = "> "
		}

		// Name with type indicator
		name := c.Name
		typeTag := ""
		switch c.Type {
		case state.TypeHeadless:
			typeTag = " [headless]"
		case state.TypeMulti:
			typeTag = " [multi]"
		case state.TypeMultiChild:
			typeTag = " [" + c.Parent + "]"
		}

		// Status indicator
		var indicator string
		if item.TmuxAlive {
			indicator = statusAlive.Render("●")
		} else {
			indicator = statusDead.Render("○")
		}

		// Age
		age := formatAge(time.Since(c.CreatedAt))

		line := fmt.Sprintf("%s%-30s %-11s %s  %s", prefix, name, typeTag, indicator, age)

		if i == m.cursor {
			line = selectedStyle.Render(line)
		} else if c.Type == state.TypeHeadless {
			line = headlessStyle.Render(line)
		}

		b.WriteString(line + "\n")
	}

	// Filter line
	b.WriteString("\n")
	count := fmt.Sprintf("  %d/%d", len(m.filtered), len(m.items))
	b.WriteString(dimStyle.Render(count) + "  " + m.filter + "█\n")

	// Help
	b.WriteString(helpStyle.Render("  enter switch  esc cancel") + "\n")

	return b.String()
}

func (m *SwitcherModel) applyFilter() {
	if m.filter == "" {
		m.filtered = m.items
	} else {
		f := strings.ToLower(m.filter)
		m.filtered = nil
		for _, item := range m.items {
			if strings.Contains(strings.ToLower(item.Cell.Name), f) {
				m.filtered = append(m.filtered, item)
			}
		}
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = max(len(m.filtered)-1, 0)
	}
}
