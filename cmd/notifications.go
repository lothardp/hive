package cmd

import (
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/lothardp/hive/internal/state"
	"github.com/spf13/cobra"
)

var notifCell string
var notifUnread bool
var notifDetail bool

var notificationsCmd = &cobra.Command{
	Use:     "notifications [id]",
	Aliases: []string{"notifs"},
	Short:   "List recent notifications or view one in detail",
	Args:    cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := cmd.Context()

		// Detail mode: view a single notification
		if len(args) == 1 {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid notification ID: %s", args[0])
			}
			notif, err := app.NotifRepo.GetByID(ctx, id)
			if err != nil {
				return fmt.Errorf("looking up notification: %w", err)
			}
			if notif == nil {
				return fmt.Errorf("notification %d not found", id)
			}
			printNotificationDetail(notif)
			return nil
		}

		// List mode
		notifs, err := app.NotifRepo.List(ctx, notifCell, notifUnread)
		if err != nil {
			return fmt.Errorf("listing notifications: %w", err)
		}

		if len(notifs) == 0 {
			fmt.Println("No notifications.")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

		if notifDetail {
			fmt.Fprintln(w, "ID\tCELL\tTITLE\tMESSAGE\tDETAILS\tSTATUS\tAGE")
		} else {
			fmt.Fprintln(w, "ID\tCELL\tTITLE\tMESSAGE\tSTATUS\tAGE")
		}

		for _, n := range notifs {
			status := "unread"
			if n.Read {
				status = "read"
			}
			age := formatAge(time.Since(n.CreatedAt))

			title := n.Title
			if title == "" {
				title = "-"
			}
			msg := n.Message
			if len(msg) > 50 {
				msg = msg[:47] + "..."
			}

			if notifDetail {
				details := n.Details
				if details == "" {
					details = "-"
				} else if len(details) > 50 {
					details = details[:47] + "..."
				}
				fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
					n.ID, n.CellName, title, msg, details, status, age)
			} else {
				fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\n",
					n.ID, n.CellName, title, msg, status, age)
			}
		}

		w.Flush()
		return nil
	},
}

func printNotificationDetail(n *state.Notification) {
	fmt.Printf("ID:      %d\n", n.ID)
	fmt.Printf("Cell:    %s\n", n.CellName)
	if n.Title != "" {
		fmt.Printf("Title:   %s\n", n.Title)
	}
	fmt.Printf("Message: %s\n", n.Message)
	if n.Details != "" {
		fmt.Printf("Details: %s\n", n.Details)
	}
	status := "unread"
	if n.Read {
		status = "read"
	}
	fmt.Printf("Status:  %s\n", status)
	fmt.Printf("Age:     %s\n", formatAge(time.Since(n.CreatedAt)))
}

func init() {
	notificationsCmd.Flags().StringVarP(&notifCell, "cell", "c", "", "Filter by cell name")
	notificationsCmd.Flags().BoolVarP(&notifUnread, "unread", "u", false, "Show only unread")
	notificationsCmd.Flags().BoolVar(&notifDetail, "detail", false, "Show details column in list")
	rootCmd.AddCommand(notificationsCmd)
}
