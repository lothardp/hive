package keybindings

// GenerateTmuxConf returns the tmux config content for Hive keybindings.
func GenerateTmuxConf() string {
	return `# Hive — switch to dashboard
bind-key h switch-client -t hive

# Hive — fuzzy cell switcher
bind-key . display-popup -E -w 60% -h 50% -T " Switch Cell " "hive switch"
`
}
