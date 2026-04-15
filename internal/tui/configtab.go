package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// ConfigModel manages the config tab.
type ConfigModel struct {
	content string // raw config file content
	hiveDir string
	editor  string
	message string
}

func NewConfigModel(hiveDir, editor string) ConfigModel {
	return ConfigModel{
		hiveDir: hiveDir,
		editor:  editor,
	}
}

// Messages

type configLoaded struct{ content string }
type configEditorFinished struct{ err error }

func (m ConfigModel) LoadConfig() tea.Cmd {
	return func() tea.Msg {
		path := filepath.Join(m.hiveDir, "config.yaml")
		data, err := os.ReadFile(path)
		if err != nil {
			return configLoaded{content: fmt.Sprintf("(could not read config: %v)", err)}
		}
		return configLoaded{content: string(data)}
	}
}

// Update handles messages for the config tab.
func (m ConfigModel) Update(msg tea.Msg) (ConfigModel, tea.Cmd) {
	switch msg := msg.(type) {
	case configLoaded:
		m.content = msg.content
		return m, nil

	case configEditorFinished:
		if msg.err != nil {
			m.message = fmt.Sprintf("Editor error: %v", msg.err)
		} else {
			m.message = "Config saved"
		}
		// Reload after editing
		return m, m.LoadConfig()

	case tea.KeyMsg:
		return m.updateKeys(msg)
	}
	return m, nil
}

func (m ConfigModel) updateKeys(msg tea.KeyMsg) (ConfigModel, tea.Cmd) {
	if key.Matches(msg, cfgKeys.Edit) {
		cfgPath := filepath.Join(m.hiveDir, "config.yaml")
		c := exec.Command(m.editor, cfgPath)
		return m, tea.ExecProcess(c, func(err error) tea.Msg {
			return configEditorFinished{err: err}
		})
	}
	return m, nil
}

// View renders the config tab.
func (m ConfigModel) View(width int) string {
	var b strings.Builder

	b.WriteString(fmt.Sprintf("  %s\n", configLabel.Render("Global Config (~/.hive/config.yaml)")))
	b.WriteString(fmt.Sprintf("  %s\n\n", strings.Repeat("─", 40)))

	for _, line := range strings.Split(m.content, "\n") {
		b.WriteString("  " + line + "\n")
	}

	return b.String()
}

// Footer returns help text for the config tab.
func (m ConfigModel) Footer() string {
	if m.message != "" {
		return m.message
	}
	return helpStyle.Render("e edit config  q quit")
}

// Key bindings

type cfgKeyMap struct {
	Edit key.Binding
}

var cfgKeys = cfgKeyMap{
	Edit: key.NewBinding(key.WithKeys("e")),
}
