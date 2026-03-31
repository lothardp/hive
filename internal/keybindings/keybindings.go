package keybindings

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/lothardp/hive/internal/shell"
)

const DefaultLeader = "h"

// Generate returns the contents of the Hive tmux.conf file.
// If tmuxVersion < 3.2, bindings are commented out with a warning.
// A tmuxVersion of 0 means unknown (generate normally).
func Generate(leader string, tmuxVersion float64) string {
	var b strings.Builder

	b.WriteString("# Hive tmux configuration\n")
	b.WriteString("# This file is managed by Hive. Regenerate with: hive keybindings\n")

	if tmuxVersion > 0 && tmuxVersion < 3.2 {
		fmt.Fprintf(&b, "# WARNING: tmux 3.2+ required for popup keybindings. Current version: %.1f\n", tmuxVersion)
		b.WriteString("# Upgrade tmux to enable keybindings.\n")
		b.WriteString("#\n")
		fmt.Fprintf(&b, "# Keybindings: <prefix> %s, then:\n", leader)
		b.WriteString("#   s — switch cell (fzf picker)\n")
		b.WriteString("#   c — create cell (name prompt)\n")
		b.WriteString("#   k — kill current cell (with confirmation)\n")
		b.WriteString("#   d — status dashboard\n")
		b.WriteString("#\n")
		fmt.Fprintf(&b, "# bind-key %s switch-client -T hive\n", leader)
		b.WriteString("#\n")
		b.WriteString("# bind-key -T hive s display-popup -E -w 60%% -h 50%% -T \" Switch Cell \" \"hive switch\"\n")
		b.WriteString("# bind-key -T hive c display-popup -E -w 60%% -h 40%% -T \" Create Cell \" \"hive cell --interactive\"\n")
		b.WriteString("# bind-key -T hive k display-popup -E -w 50%% -h 30%% -T \" Kill Cell \" \"hive kill --current\"\n")
		b.WriteString("# bind-key -T hive d display-popup -E -w 80%% -h 50%% -T \" Hive Status \" \"hive status --persist\"\n")
		return b.String()
	}

	b.WriteString("#\n")
	fmt.Fprintf(&b, "# Keybindings: <prefix> %s, then:\n", leader)
	b.WriteString("#   s — switch cell (fzf picker)\n")
	b.WriteString("#   c — create cell (name prompt)\n")
	b.WriteString("#   k — kill current cell (with confirmation)\n")
	b.WriteString("#   d — status dashboard\n")
	b.WriteString("\n")

	// Enter Hive key table
	b.WriteString("# Enter Hive key table\n")
	fmt.Fprintf(&b, "bind-key %s switch-client -T hive\n", leader)
	b.WriteString("\n")

	// Hive key table bindings
	b.WriteString("# Hive key table bindings\n")
	b.WriteString(`bind-key -T hive s display-popup -E -w 60% -h 50% -T " Switch Cell " "hive switch"`)
	b.WriteString("\n")
	b.WriteString(`bind-key -T hive c display-popup -E -w 60% -h 40% -T " Create Cell " "hive cell --interactive"`)
	b.WriteString("\n")
	b.WriteString(`bind-key -T hive k display-popup -E -w 50% -h 30% -T " Kill Cell " "hive kill --current"`)
	b.WriteString("\n")
	b.WriteString(`bind-key -T hive d display-popup -E -w 80% -h 50% -T " Hive Status " "hive status --persist"`)
	b.WriteString("\n")

	return b.String()
}

// TmuxVersion parses the tmux version from `tmux -V` output.
// Returns 0 if tmux is not installed or version cannot be parsed.
func TmuxVersion(ctx context.Context) (float64, error) {
	res, err := shell.Run(ctx, "tmux", "-V")
	if err != nil {
		return 0, fmt.Errorf("running tmux -V: %w", err)
	}
	if res.ExitCode != 0 {
		return 0, fmt.Errorf("tmux -V exited with code %d", res.ExitCode)
	}
	return ParseTmuxVersion(strings.TrimSpace(res.Stdout))
}

// ParseTmuxVersion extracts a float64 version from a tmux -V output string.
// Examples: "tmux 3.4" → 3.4, "tmux 3.2a" → 3.2, "tmux next-3.5" → 3.5
func ParseTmuxVersion(output string) (float64, error) {
	re := regexp.MustCompile(`(\d+\.\d+)`)
	match := re.FindString(output)
	if match == "" {
		return 0, fmt.Errorf("could not parse tmux version from %q", output)
	}
	v, err := strconv.ParseFloat(match, 64)
	if err != nil {
		return 0, fmt.Errorf("parsing version %q: %w", match, err)
	}
	return v, nil
}

// Bindings returns a summary of keybindings for display.
func Bindings(leader string) []string {
	return []string{
		fmt.Sprintf("<prefix> %s s — switch cell", leader),
		fmt.Sprintf("<prefix> %s c — create cell", leader),
		fmt.Sprintf("<prefix> %s k — kill current cell", leader),
		fmt.Sprintf("<prefix> %s d — status dashboard", leader),
	}
}
