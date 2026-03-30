package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var initProxyCmd = &cobra.Command{
	Use:   "init-proxy",
	Short: "Start the global Caddy reverse proxy",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("not implemented: init-proxy")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(initProxyCmd)
}
