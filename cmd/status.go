package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/lothardp/hive/internal/state"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show overview of all cells (the comb)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		cells, err := app.Repo.List(ctx)
		if err != nil {
			return fmt.Errorf("listing cells: %w", err)
		}

		if len(cells) == 0 {
			fmt.Println("No cells. Create one with: hive cell <name>")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "NAME\tPROJECT\tBRANCH\tSTATUS\tTMUX\tPORTS\tAGE")

		for _, c := range cells {
			tmuxStatus := "dead"
			exists, err := app.TmuxMgr.SessionExists(ctx, c.Name)
			if err == nil && exists {
				tmuxStatus = "alive"
			}

			age := formatAge(time.Since(c.CreatedAt))
			portsDisplay := formatPortsColumn(c.Ports)

			nameDisplay := c.Name
			switch c.Type {
			case state.TypeQueen:
				nameDisplay += " [queen]"
			case state.TypeHeadless:
				nameDisplay += " [headless]"
			}

			project := c.Project
			if project == "" {
				project = "-"
			}
			branch := c.Branch
			if branch == "" {
				branch = "-"
			}

			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				nameDisplay, project, branch, c.Status, tmuxStatus, portsDisplay, age)
		}

		w.Flush()
		return nil
	},
}

func formatAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// formatPortsColumn parses the ports JSON and returns a comma-separated list of port numbers.
func formatPortsColumn(portsJSON string) string {
	if portsJSON == "" || portsJSON == "{}" {
		return "-"
	}
	var portMap map[string]int
	if err := json.Unmarshal([]byte(portsJSON), &portMap); err != nil {
		return "-"
	}
	if len(portMap) == 0 {
		return "-"
	}
	vals := make([]int, 0, len(portMap))
	for _, v := range portMap {
		vals = append(vals, v)
	}
	sort.Ints(vals)
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, ",")
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
