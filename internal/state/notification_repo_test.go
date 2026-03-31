package state

import (
	"context"
	"testing"
)

func setupNotifTestDB(t *testing.T) (*NotificationRepository, *CellRepository) {
	t.Helper()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("opening test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewNotificationRepository(db), NewCellRepository(db)
}

func createTestCell(t *testing.T, cellRepo *CellRepository, name string) {
	t.Helper()
	ctx := context.Background()
	cell := &Cell{
		Name:         name,
		Project:      "testproj",
		Branch:       "main",
		WorktreePath: "/tmp/" + name,
		Status:       StatusRunning,
		Ports:        "{}",
	}
	if err := cellRepo.Create(ctx, cell); err != nil {
		t.Fatalf("creating test cell %s: %v", name, err)
	}
}

func TestNotificationCreate(t *testing.T) {
	notifRepo, cellRepo := setupNotifTestDB(t)
	ctx := context.Background()
	createTestCell(t, cellRepo, "test-cell")

	n := &Notification{
		CellName: "test-cell",
		Title:    "Build done",
		Message:  "All tests passed",
		Details:  "See /tmp/results.log",
	}
	if err := notifRepo.Create(ctx, n); err != nil {
		t.Fatalf("creating notification: %v", err)
	}
	if n.ID == 0 {
		t.Fatal("expected non-zero ID")
	}
	if n.CreatedAt.IsZero() {
		t.Fatal("expected non-zero CreatedAt")
	}
}

func TestNotificationGetByID(t *testing.T) {
	notifRepo, cellRepo := setupNotifTestDB(t)
	ctx := context.Background()
	createTestCell(t, cellRepo, "test-cell")

	n := &Notification{CellName: "test-cell", Title: "T", Message: "M", Details: "D"}
	if err := notifRepo.Create(ctx, n); err != nil {
		t.Fatalf("creating: %v", err)
	}

	got, err := notifRepo.GetByID(ctx, n.ID)
	if err != nil {
		t.Fatalf("getting: %v", err)
	}
	if got == nil {
		t.Fatal("expected notification, got nil")
	}
	if got.Title != "T" {
		t.Errorf("expected title T, got %s", got.Title)
	}
	if got.Message != "M" {
		t.Errorf("expected message M, got %s", got.Message)
	}
	if got.Details != "D" {
		t.Errorf("expected details D, got %s", got.Details)
	}
	if got.Read {
		t.Error("expected unread")
	}
}

func TestNotificationGetByIDNotFound(t *testing.T) {
	notifRepo, _ := setupNotifTestDB(t)
	ctx := context.Background()

	got, err := notifRepo.GetByID(ctx, 999)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestNotificationList(t *testing.T) {
	notifRepo, cellRepo := setupNotifTestDB(t)
	ctx := context.Background()
	createTestCell(t, cellRepo, "cell-a")
	createTestCell(t, cellRepo, "cell-b")

	for _, n := range []*Notification{
		{CellName: "cell-a", Message: "msg1"},
		{CellName: "cell-a", Message: "msg2"},
		{CellName: "cell-b", Message: "msg3"},
	} {
		if err := notifRepo.Create(ctx, n); err != nil {
			t.Fatalf("creating: %v", err)
		}
	}

	// All notifications
	all, err := notifRepo.List(ctx, "", false)
	if err != nil {
		t.Fatalf("listing all: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 3, got %d", len(all))
	}

	// Filter by cell
	cellA, err := notifRepo.List(ctx, "cell-a", false)
	if err != nil {
		t.Fatalf("listing cell-a: %v", err)
	}
	if len(cellA) != 2 {
		t.Fatalf("expected 2 for cell-a, got %d", len(cellA))
	}

	cellB, err := notifRepo.List(ctx, "cell-b", false)
	if err != nil {
		t.Fatalf("listing cell-b: %v", err)
	}
	if len(cellB) != 1 {
		t.Fatalf("expected 1 for cell-b, got %d", len(cellB))
	}
}

func TestNotificationListUnreadOnly(t *testing.T) {
	notifRepo, cellRepo := setupNotifTestDB(t)
	ctx := context.Background()
	createTestCell(t, cellRepo, "cell-a")

	for _, msg := range []string{"m1", "m2", "m3"} {
		n := &Notification{CellName: "cell-a", Message: msg}
		if err := notifRepo.Create(ctx, n); err != nil {
			t.Fatalf("creating: %v", err)
		}
	}

	// Mark one as read
	if _, err := notifRepo.MarkReadByCell(ctx, "cell-a"); err != nil {
		t.Fatalf("marking read: %v", err)
	}

	// All should be read now
	unread, err := notifRepo.List(ctx, "", true)
	if err != nil {
		t.Fatalf("listing unread: %v", err)
	}
	if len(unread) != 0 {
		t.Fatalf("expected 0 unread, got %d", len(unread))
	}
}

func TestNotificationMarkReadByCell(t *testing.T) {
	notifRepo, cellRepo := setupNotifTestDB(t)
	ctx := context.Background()
	createTestCell(t, cellRepo, "cell-a")
	createTestCell(t, cellRepo, "cell-b")

	for _, n := range []*Notification{
		{CellName: "cell-a", Message: "a1"},
		{CellName: "cell-a", Message: "a2"},
		{CellName: "cell-b", Message: "b1"},
	} {
		if err := notifRepo.Create(ctx, n); err != nil {
			t.Fatalf("creating: %v", err)
		}
	}

	count, err := notifRepo.MarkReadByCell(ctx, "cell-a")
	if err != nil {
		t.Fatalf("marking read: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 marked read, got %d", count)
	}

	// cell-b should still be unread
	unreadB, err := notifRepo.CountUnread(ctx, "cell-b")
	if err != nil {
		t.Fatalf("counting: %v", err)
	}
	if unreadB != 1 {
		t.Errorf("expected 1 unread for cell-b, got %d", unreadB)
	}

	// Marking again should return 0
	count2, err := notifRepo.MarkReadByCell(ctx, "cell-a")
	if err != nil {
		t.Fatalf("marking read again: %v", err)
	}
	if count2 != 0 {
		t.Errorf("expected 0 on second mark, got %d", count2)
	}
}

func TestNotificationCountUnread(t *testing.T) {
	notifRepo, cellRepo := setupNotifTestDB(t)
	ctx := context.Background()
	createTestCell(t, cellRepo, "cell-a")
	createTestCell(t, cellRepo, "cell-b")

	for _, n := range []*Notification{
		{CellName: "cell-a", Message: "a1"},
		{CellName: "cell-a", Message: "a2"},
		{CellName: "cell-b", Message: "b1"},
	} {
		if err := notifRepo.Create(ctx, n); err != nil {
			t.Fatalf("creating: %v", err)
		}
	}

	// Total unread
	total, err := notifRepo.CountUnread(ctx, "")
	if err != nil {
		t.Fatalf("counting total: %v", err)
	}
	if total != 3 {
		t.Errorf("expected 3 total unread, got %d", total)
	}

	// Per cell
	countA, err := notifRepo.CountUnread(ctx, "cell-a")
	if err != nil {
		t.Fatalf("counting cell-a: %v", err)
	}
	if countA != 2 {
		t.Errorf("expected 2 for cell-a, got %d", countA)
	}

	// After marking read
	notifRepo.MarkReadByCell(ctx, "cell-a")
	countA2, err := notifRepo.CountUnread(ctx, "cell-a")
	if err != nil {
		t.Fatalf("counting cell-a after mark: %v", err)
	}
	if countA2 != 0 {
		t.Errorf("expected 0 for cell-a after mark, got %d", countA2)
	}
}

func TestNotificationCascadeDelete(t *testing.T) {
	notifRepo, cellRepo := setupNotifTestDB(t)
	ctx := context.Background()
	createTestCell(t, cellRepo, "doomed")

	n := &Notification{CellName: "doomed", Message: "bye"}
	if err := notifRepo.Create(ctx, n); err != nil {
		t.Fatalf("creating: %v", err)
	}

	// Delete the cell — notifications should cascade
	if err := cellRepo.Delete(ctx, "doomed"); err != nil {
		t.Fatalf("deleting cell: %v", err)
	}

	got, err := notifRepo.GetByID(ctx, n.ID)
	if err != nil {
		t.Fatalf("getting after cascade: %v", err)
	}
	if got != nil {
		t.Fatal("expected notification to be cascade-deleted")
	}
}

func TestNotificationListOrder(t *testing.T) {
	notifRepo, cellRepo := setupNotifTestDB(t)
	ctx := context.Background()
	createTestCell(t, cellRepo, "cell")

	for _, msg := range []string{"first", "second", "third"} {
		n := &Notification{CellName: "cell", Message: msg}
		if err := notifRepo.Create(ctx, n); err != nil {
			t.Fatalf("creating: %v", err)
		}
	}

	all, err := notifRepo.List(ctx, "", false)
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	// Most recent first (DESC)
	if all[0].Message != "third" {
		t.Errorf("expected third first, got %s", all[0].Message)
	}
	if all[2].Message != "first" {
		t.Errorf("expected first last, got %s", all[2].Message)
	}
}
