package e2e_test

import "testing"

func TestStartGame(t *testing.T) {
	resetGame(t)
	openRoom(t)
	joinPlayer(t, "Alice")
	joinPlayer(t, "Bob")
	startGame(t)

	// Status should now be "playing".
	got := apiGet(t, "/players")
	if got["status"] != "playing" {
		t.Fatalf("expected status=playing after start, got %v", got["status"])
	}
}

func TestPlayerReceivesMissions(t *testing.T) {
	resetGame(t)
	openRoom(t)
	joinPlayer(t, "Alice")
	startGame(t)

	me := apiGet(t, "/me/Alice")
	missions, ok := me["missions"].([]any)
	if !ok {
		t.Fatalf("missions field missing: %v", me)
	}
	// Should have 4 normal + 1 hard = 5 missions.
	if len(missions) != 5 {
		t.Fatalf("expected 5 missions, got %d", len(missions))
	}
	abilities, ok := me["abilities"].([]any)
	if !ok {
		t.Fatalf("abilities field missing: %v", me)
	}
	if len(abilities) != 2 {
		t.Fatalf("expected 2 ability cards, got %d", len(abilities))
	}
}

// TestStartWithoutOpen verifies that /start without /admin/open returns 400.
func TestStartWithoutOpen(t *testing.T) {
	resetGame(t)

	code := apiPostRaw(t, "/start", map[string]int{"duration_minutes": 60})
	if code != 400 {
		t.Fatalf("expected 400 when starting without open, got %d", code)
	}
}
