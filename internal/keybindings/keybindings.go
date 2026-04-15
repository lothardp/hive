package keybindings

// GenerateTmuxConf returns the tmux config content for Hive keybindings.
func GenerateTmuxConf() string {
	return "# Hive — switch to dashboard\nbind-key h switch-client -t hive\n"
}
