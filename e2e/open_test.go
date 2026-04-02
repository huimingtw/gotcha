package e2e_test

import "testing"

// TestAdminOpenRoom verifies that after /admin/open the status becomes "waiting".
func TestAdminOpenRoom(t *testing.T) {
	resetGame(t)

	got := apiGet(t, "/players")
	if got["status"] != "closed" {
		t.Fatalf("expected status=closed after reset, got %v", got["status"])
	}

	openRoom(t)

	got = apiGet(t, "/players")
	if got["status"] != "waiting" {
		t.Fatalf("expected status=waiting after open, got %v", got["status"])
	}
}

// TestAdminOpenRoomIdempotent verifies that opening an already-open room returns 400.
func TestAdminOpenRoomIdempotent(t *testing.T) {
	resetGame(t)
	openRoom(t)

	resp := apiPostRaw(t, "/admin/open", nil)
	if resp != 400 {
		t.Fatalf("expected 400 on double open, got %d", resp)
	}
}
