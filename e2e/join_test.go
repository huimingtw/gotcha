package e2e_test

import "testing"

func TestPlayerJoin(t *testing.T) {
	resetGame(t)
	openRoom(t)

	joinPlayer(t, "Alice")
	joinPlayer(t, "Bob")

	got := apiGet(t, "/players")
	players, ok := got["players"].([]any)
	if !ok {
		t.Fatalf("players field missing or wrong type: %v", got)
	}
	if len(players) != 2 {
		t.Fatalf("expected 2 players, got %d", len(players))
	}
}

// TestJoinClosedRoom verifies that joining a closed room returns 400.
func TestJoinClosedRoom(t *testing.T) {
	resetGame(t)

	code := apiPostRaw(t, "/join", map[string]string{"name": "Alice"})
	if code != 400 {
		t.Fatalf("expected 400 when joining closed room, got %d", code)
	}
}
