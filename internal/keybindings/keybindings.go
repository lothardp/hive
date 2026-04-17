package keybindings

// GenerateTmuxConf returns the tmux config content for Hive keybindings.
func GenerateTmuxConf() string {
	return `# Hive — switch to dashboard
bind-key . switch-client -t hive

# Hive — fuzzy cell switcher
bind-key f run-shell "tmux neww hive switch"
# bind-key f display-popup -E -w 60% -h 50% -T " Switch Cell " "hive switch"

# Hive — notifications
bind-key n run-shell "tmux neww hive dashboard --tab notifs"
`
}
