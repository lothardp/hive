package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var jumpCmd = &cobra.Command{
	Use:               "jump <cell>",
	Short:             "Mark notifications read and attach to a cell",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeCellNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("not implemented: jump", args[0])
		return nil
	},
}

func init() {
	rootCmd.AddCommand(jumpCmd)
}
