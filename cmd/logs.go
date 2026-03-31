package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"
)

var (
	logsTail   int
	logsFollow bool
	logsClear  bool
)

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Show Hive logs",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		logPath := filepath.Join(app.HiveDir, "hive.log")

		if logsClear {
			if err := os.Truncate(logPath, 0); err != nil {
				if os.IsNotExist(err) {
					fmt.Println("No log file to clear")
					return nil
				}
				return fmt.Errorf("clearing logs: %w", err)
			}
			fmt.Println("Logs cleared")
			return nil
		}

		if _, err := os.Stat(logPath); os.IsNotExist(err) {
			fmt.Println("No logs yet")
			return nil
		}

		if logsFollow {
			// Use tail -f for live following
			tailCmd := exec.Command("tail", "-f", "-n", strconv.Itoa(logsTail), logPath)
			tailCmd.Stdout = os.Stdout
			tailCmd.Stderr = os.Stderr
			return tailCmd.Run()
		}

		// Show last N lines
		tailCmd := exec.Command("tail", "-n", strconv.Itoa(logsTail), logPath)
		tailCmd.Stdout = os.Stdout
		tailCmd.Stderr = os.Stderr
		return tailCmd.Run()
	},
}

func init() {
	logsCmd.Flags().IntVarP(&logsTail, "lines", "n", 50, "Number of lines to show")
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "Follow log output (tail -f)")
	logsCmd.Flags().BoolVar(&logsClear, "clear", false, "Clear the log file")
	rootCmd.AddCommand(logsCmd)
}
