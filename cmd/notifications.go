package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var notificationsCmd = &cobra.Command{
	Use:   "notifications",
	Short: "List recent notifications",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("not implemented: notifications")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(notificationsCmd)
}
