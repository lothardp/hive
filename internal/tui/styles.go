package tui

import "github.com/charmbracelet/lipgloss"

var (
	projectStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("4"))
	headlessStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	selectedStyle = lipgloss.NewStyle().Background(lipgloss.Color("8")).Bold(true)
	statusAlive   = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	statusDead    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	unreadStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true)
	helpStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	titleStyle    = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	confirmStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	activeTab     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("4")).Underline(true)
	inactiveTab   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	configLabel   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	successStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	progressStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
)
