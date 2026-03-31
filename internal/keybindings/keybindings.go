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
// If direct is true, bindings are mapped directly to <prefix> <key>
// instead of going through an intermediate hive key table.
func Generate(leader string, tmuxVersion float64, direct bool) string {
	var b strings.Builder

	b.WriteString("# Hive tmux configuration\n")
	b.WriteString("# This file is managed by Hive. Regenerate with: hive keybindings\n")

	if direct {
		return generateDirect(&b, tmuxVersion)
	}
	return generateTable(&b, leader, tmuxVersion)
}

func generateDirect(b *strings.Builder, tmuxVersion float64) string {
	keys := directKeys()

	if tmuxVersion > 0 && tmuxVersion < 3.2 {
		fmt.Fprintf(b, "# WARNING: tmux 3.2+ required for popup keybindings. Current version: %.1f\n", tmuxVersion)
		b.WriteString("# Upgrade tmux to enable keybindings.\n")
		b.WriteString("#\n")
		b.WriteString("# Keybindings (direct mode):\n")
		for _, k := range keys {
			fmt.Fprintf(b, "#   <prefix> %s — %s\n", k.key, k.desc)
		}
		b.WriteString("#\n")
		for _, k := range keys {
			fmt.Fprintf(b, "# bind-key %s display-popup -E %s -T \" %s \" \"%s\"\n", k.key, k.size, k.title, k.cmd)
		}
		return b.String()
	}

	b.WriteString("#\n")
	b.WriteString("# Keybindings (direct mode):\n")
	for _, k := range keys {
		fmt.Fprintf(b, "#   <prefix> %s — %s\n", k.key, k.desc)
	}
	b.WriteString("\n")

	b.WriteString("# Direct keybindings\n")
	for _, k := range keys {
		fmt.Fprintf(b, "bind-key %s display-popup -E %s -T \" %s \" \"%s\"\n", k.key, k.size, k.title, k.cmd)
	}

	return b.String()
}

func generateTable(b *strings.Builder, leader string, tmuxVersion float64) string {
	keys := directKeys()

	if tmuxVersion > 0 && tmuxVersion < 3.2 {
		fmt.Fprintf(b, "# WARNING: tmux 3.2+ required for popup keybindings. Current version: %.1f\n", tmuxVersion)
		b.WriteString("# Upgrade tmux to enable keybindings.\n")
		b.WriteString("#\n")
		fmt.Fprintf(b, "# Keybindings: <prefix> %s, then:\n", leader)
		for _, k := range keys {
			fmt.Fprintf(b, "#   %s — %s\n", k.key, k.desc)
		}
		b.WriteString("#\n")
		fmt.Fprintf(b, "# bind-key %s switch-client -T hive\n", leader)
		b.WriteString("#\n")
		for _, k := range keys {
			fmt.Fprintf(b, "# bind-key -T hive %s display-popup -E %s -T \" %s \" \"%s\"\n", k.key, k.size, k.title, k.cmd)
		}
		return b.String()
	}

	b.WriteString("#\n")
	fmt.Fprintf(b, "# Keybindings: <prefix> %s, then:\n", leader)
	for _, k := range keys {
		fmt.Fprintf(b, "#   %s — %s\n", k.key, k.desc)
	}
	b.WriteString("\n")

	// Enter Hive key table
	b.WriteString("# Enter Hive key table\n")
	fmt.Fprintf(b, "bind-key %s switch-client -T hive\n", leader)
	b.WriteString("\n")

	// Hive key table bindings
	b.WriteString("# Hive key table bindings\n")
	for _, k := range keys {
		fmt.Fprintf(b, "bind-key -T hive %s display-popup -E %s -T \" %s \" \"%s\"\n", k.key, k.size, k.title, k.cmd)
	}

	return b.String()
}

type binding struct {
	key   string
	desc  string
	size  string
	title string
	cmd   string
}

func directKeys() []binding {
	return []binding{
		{key: "s", desc: "switch cell (fzf picker)", size: "-w 60% -h 50%", title: "Switch Cell", cmd: "hive switch"},
		{key: "c", desc: "create cell (name prompt)", size: "-w 60% -h 40%", title: "Create Cell", cmd: "hive cell --interactive"},
		{key: "k", desc: "kill current cell (with confirmation)", size: "-w 50% -h 30%", title: "Kill Cell", cmd: "hive kill --current"},
		{key: "d", desc: "status dashboard", size: "-w 80% -h 50%", title: "Hive Status", cmd: "hive status --persist"},
	}
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
func Bindings(leader string, direct bool) []string {
	keys := directKeys()
	out := make([]string, len(keys))
	for i, k := range keys {
		if direct {
			out[i] = fmt.Sprintf("<prefix> %s — %s", k.key, k.desc)
		} else {
			out[i] = fmt.Sprintf("<prefix> %s %s — %s", leader, k.key, k.desc)
		}
	}
	return out
}
