package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

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
		fmt.Fprintln(w, "NAME\tPROJECT\tBRANCH\tSTATUS\tTMUX\tAGE")

		for _, c := range cells {
			tmuxStatus := "dead"
			exists, err := app.TmuxMgr.SessionExists(ctx, c.Name)
			if err == nil && exists {
				tmuxStatus = "alive"
			}

			age := formatAge(time.Since(c.CreatedAt))
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
				c.Name, c.Project, c.Branch, c.Status, tmuxStatus, age)
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

func init() {
	rootCmd.AddCommand(statusCmd)
}
