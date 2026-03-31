package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/lothardp/hive/internal/shell"
	"github.com/lothardp/hive/internal/state"
	"github.com/spf13/cobra"
)

var notifyTitle string
var notifyDetails string

var notifyCmd = &cobra.Command{
	Use:   "notify <message>",
	Short: "Send a notification from the current cell",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()
		message := args[0]

		cellName := os.Getenv("HIVE_CELL")
		if cellName == "" {
			return fmt.Errorf("not inside a Hive cell (HIVE_CELL not set)")
		}

		cell, err := app.Repo.GetByName(ctx, cellName)
		if err != nil {
			return fmt.Errorf("looking up cell: %w", err)
		}
		if cell == nil {
			return fmt.Errorf("cell %q not found in DB", cellName)
		}

		notif := &state.Notification{
			CellName: cellName,
			Title:    notifyTitle,
			Message:  message,
			Details:  notifyDetails,
		}
		if err := app.NotifRepo.Create(ctx, notif); err != nil {
			return fmt.Errorf("creating notification: %w", err)
		}

		if err := sendSystemNotification(ctx, cellName, notifyTitle, message); err != nil {
			slog.Warn("system notification failed", "error", err)
		}

		fmt.Printf("Notification sent from %s\n", cellName)
		return nil
	},
}

func sendSystemNotification(ctx context.Context, cellName, title, message string) error {
	bannerTitle := fmt.Sprintf("Hive: %s", cellName)
	if title != "" {
		bannerTitle = fmt.Sprintf("Hive: %s", title)
	}
	script := fmt.Sprintf(`display notification %q with title %q`, message, bannerTitle)
	_, err := shell.Run(ctx, "osascript", "-e", script)
	return err
}

func init() {
	notifyCmd.Flags().StringVarP(&notifyTitle, "title", "t", "", "Short headline")
	notifyCmd.Flags().StringVarP(&notifyDetails, "details", "d", "", "Detailed context")
	rootCmd.AddCommand(notifyCmd)
}
