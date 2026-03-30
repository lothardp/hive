package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var swarmCmd = &cobra.Command{
	Use:   "swarm <name1> <name2> ...",
	Short: "Spin up multiple cells at once",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("not implemented: swarm", args)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(swarmCmd)
}
