package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var notifyCmd = &cobra.Command{
	Use:   "notify <message>",
	Short: "Send a notification from the current cell",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("not implemented: notify", args[0])
		return nil
	},
}

func init() {
	rootCmd.AddCommand(notifyCmd)
}
