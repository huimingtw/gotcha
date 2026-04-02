package e2e_test

import "testing"

func TestAdminEndGame(t *testing.T) {
	resetGame(t)
	openRoom(t)
	joinPlayer(t, "Alice")
	joinPlayer(t, "Bob")
	startGame(t)

	// Complete one mission for Alice.
	me := apiGet(t, "/me/Alice")
	missions := me["missions"].([]any)
	pmID := int(missions[0].(map[string]any)["pm_id"].(float64))
	apiPost(t, "/complete", map[string]any{
		"player_name":  "Alice",
		"pm_id":        pmID,
		"participants": 1,
	})

	// End game.
	apiPost(t, "/admin/end", nil)

	got := apiGet(t, "/api/leaderboard")
	if got["status"] != "ended" {
		t.Fatalf("expected status=ended, got %v", got["status"])
	}

	// Alice should appear in leaderboard with score > 0.
	entries, ok := got["leaderboard"].([]any)
	if !ok || len(entries) == 0 {
		t.Fatalf("leaderboard empty: %v", got)
	}
	top := entries[0].(map[string]any)
	if top["name"] != "Alice" {
		t.Fatalf("expected Alice at top, got %v", top["name"])
	}
	if top["score"].(float64) < 1 {
		t.Fatalf("expected score > 0, got %v", top["score"])
	}
}

func TestLeaderboardHasLog(t *testing.T) {
	resetGame(t)
	openRoom(t)
	joinPlayer(t, "Alice")
	startGame(t)

	me := apiGet(t, "/me/Alice")
	missions := me["missions"].([]any)
	pmID := int(missions[0].(map[string]any)["pm_id"].(float64))
	apiPost(t, "/complete", map[string]any{
		"player_name":  "Alice",
		"pm_id":        pmID,
		"participants": 1,
	})

	apiPost(t, "/admin/end", nil)

	lb := apiGet(t, "/api/leaderboard")
	logEntries, ok := lb["log"].([]any)
	if !ok {
		t.Fatalf("log field missing: %v", lb)
	}
	if len(logEntries) == 0 {
		t.Fatalf("expected at least one log entry after end")
	}
}

func TestResetGame(t *testing.T) {
	resetGame(t)
	openRoom(t)
	joinPlayer(t, "Alice")

	resetGame(t)

	got := apiGet(t, "/players")
	if got["status"] != "closed" {
		t.Fatalf("expected status=closed after reset, got %v", got["status"])
	}
	players := got["players"].([]any)
	if len(players) != 0 {
		t.Fatalf("expected 0 players after reset, got %d", len(players))
	}
}
