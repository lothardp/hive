package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/lothardp/hive/internal/shell"
	"github.com/lothardp/hive/internal/state"
	"github.com/spf13/cobra"
)

var notifyTitle string
var notifyDetails string
var notifyFromClaude bool

var notifyCmd = &cobra.Command{
	Use:   "notify [message]",
	Short: "Send a notification from the current cell",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		cellName := os.Getenv("HIVE_CELL")
		if cellName == "" {
			return fmt.Errorf("not inside a Hive cell (HIVE_CELL not set)")
		}

		cell, err := app.CellRepo.GetByName(ctx, cellName)
		if err != nil {
			return fmt.Errorf("looking up cell: %w", err)
		}
		if cell == nil {
			return fmt.Errorf("cell %q not found in DB", cellName)
		}

		var message, title, details string

		if notifyFromClaude {
			message, title, details, err = parseClaudeHookInput()
			if err != nil {
				return fmt.Errorf("parsing claude hook input: %w", err)
			}
		} else {
			if len(args) == 0 {
				return fmt.Errorf("message argument is required")
			}
			message = args[0]
			title = notifyTitle
			details = notifyDetails
		}

		notif := &state.Notification{
			CellName:   cellName,
			Title:      title,
			Message:    message,
			Details:    details,
			SourcePane: os.Getenv("TMUX_PANE"),
		}
		if err := app.NotifRepo.Create(ctx, notif); err != nil {
			return fmt.Errorf("creating notification: %w", err)
		}

		if err := sendSystemNotification(ctx, cellName, message); err != nil {
			slog.Warn("system notification failed", "error", err)
		}

		fmt.Printf("Notification sent from %s\n", cellName)
		return nil
	},
}

// claudeHookInput represents the JSON input from a Claude Code hook.
type claudeHookInput struct {
	// Notification hook fields
	NotificationType string `json:"notification_type"`
	Message          string `json:"message"`
	Title            string `json:"title"`

	// Stop hook fields
	LastAssistantMessage string `json:"last_assistant_message"`
	StopHookActive       bool   `json:"stop_hook_active"`

	// Common
	HookEventName string `json:"hook_event_name"`
}

func parseClaudeHookInput() (message, title, details string, err error) {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", "", "", fmt.Errorf("reading stdin: %w", err)
	}

	var input claudeHookInput
	if err := json.Unmarshal(data, &input); err != nil {
		return "", "", "", fmt.Errorf("parsing JSON: %w", err)
	}

	switch input.HookEventName {
	case "Notification":
		message = input.Message
		if message == "" {
			message = "Needs attention"
		}
		title = fmt.Sprintf("Claude: %s", input.NotificationType)
		if input.Title != "" {
			details = input.Title
		}

	case "Stop":
		msg := input.LastAssistantMessage
		if len(msg) > 200 {
			msg = msg[:200] + "..."
		}
		if msg == "" {
			msg = "Claude stopped"
		}
		message = msg
		title = "Claude Code — done"

	default:
		message = input.Message
		if message == "" {
			message = fmt.Sprintf("Claude hook: %s", input.HookEventName)
		}
		title = fmt.Sprintf("Claude: %s", input.HookEventName)
	}

	return message, title, details, nil
}

func sendSystemNotification(ctx context.Context, cellName, message string) error {
	bannerTitle := fmt.Sprintf("Hive: %s", cellName)
	script := fmt.Sprintf(`display notification %q with title %q`, message, bannerTitle)
	_, err := shell.Run(ctx, "osascript", "-e", script)
	return err
}

func init() {
	notifyCmd.Flags().StringVarP(&notifyTitle, "title", "t", "", "Short headline")
	notifyCmd.Flags().StringVarP(&notifyDetails, "details", "d", "", "Detailed context")
	notifyCmd.Flags().BoolVar(&notifyFromClaude, "from-claude", false, "Read Claude Code hook JSON from stdin")
	rootCmd.AddCommand(notifyCmd)
}
