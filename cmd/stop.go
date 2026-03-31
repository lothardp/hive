package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:               "stop <name>",
	Short:             "Suspend a cell (stop containers, keep worktree)",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeCellNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("not implemented: stop", args[0])
		return nil
	},
}

func init() {
	rootCmd.AddCommand(stopCmd)
}
