package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var peekCmd = &cobra.Command{
	Use:               "peek <name>",
	Short:             "Show detailed status of a single cell",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeCellNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("not implemented: peek", args[0])
		return nil
	},
}

func init() {
	rootCmd.AddCommand(peekCmd)
}
