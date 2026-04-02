package e2e_test

import (
	"testing"
)

// setupGame sets up a game with one player "Alice" in playing state.
// Returns Alice's first active normal mission pm_id.
func setupGame(t *testing.T) (pmID int) {
	t.Helper()
	resetGame(t)
	openRoom(t)
	joinPlayer(t, "Alice")
	startGame(t)

	me := apiGet(t, "/me/Alice")
	missions, ok := me["missions"].([]any)
	if !ok || len(missions) == 0 {
		t.Fatalf("no missions for Alice: %v", me)
	}
	// Find first normal active mission.
	for _, raw := range missions {
		m := raw.(map[string]any)
		if m["type"] == "normal" && m["status"] == "active" {
			return int(m["pm_id"].(float64))
		}
	}
	t.Fatalf("no active normal mission found")
	return 0
}

func TestCompleteMission(t *testing.T) {
	pmID := setupGame(t)

	resp := apiPost(t, "/complete", map[string]any{
		"player_name":  "Alice",
		"pm_id":        pmID,
		"participants": 1,
	})
	if resp["ok"] != true {
		t.Fatalf("complete failed: %v", resp)
	}
	scoreEarned, ok := resp["score_earned"].(float64)
	if !ok || scoreEarned < 1 {
		t.Fatalf("expected positive score_earned, got %v", resp["score_earned"])
	}
}

func TestFailMission(t *testing.T) {
	pmID := setupGame(t)

	resp := apiPost(t, "/fail", map[string]any{
		"accuser_name": "Alice",
		"pm_id":        pmID,
	})
	if resp["ok"] != true {
		t.Fatalf("fail returned: %v", resp)
	}

	// Verify mission is now failed.
	me := apiGet(t, "/me/Alice")
	missions := me["missions"].([]any)
	for _, raw := range missions {
		m := raw.(map[string]any)
		if int(m["pm_id"].(float64)) == pmID {
			if m["status"] != "failed" {
				t.Fatalf("expected status=failed, got %v", m["status"])
			}
			return
		}
	}
	t.Fatalf("pm_id %d not found in missions", pmID)
}

func TestSwapAbility(t *testing.T) {
	pmID := setupGame(t)

	resp := apiPost(t, "/ability", map[string]any{
		"player_name": "Alice",
		"ability":     "swap",
		"pm_id":       pmID,
	})
	if resp["ok"] != true {
		t.Fatalf("swap failed: %v", resp)
	}
	if resp["action"] != "swap" {
		t.Fatalf("expected action=swap, got %v", resp["action"])
	}
}

func TestDoubleAbility(t *testing.T) {
	pmID := setupGame(t)

	resp := apiPost(t, "/ability", map[string]any{
		"player_name": "Alice",
		"ability":     "double",
		"pm_id":       pmID,
	})
	if resp["ok"] != true {
		t.Fatalf("double failed: %v", resp)
	}

	// Complete the doubled mission — should earn 4 pts (2 * 2).
	complete := apiPost(t, "/complete", map[string]any{
		"player_name":  "Alice",
		"pm_id":        pmID,
		"participants": 1,
	})
	if complete["score_earned"].(float64) != 4 {
		t.Fatalf("expected score_earned=4 with double, got %v", complete["score_earned"])
	}
}

func TestBonusUnlock(t *testing.T) {
	resetGame(t)
	openRoom(t)
	joinPlayer(t, "Alice")
	startGame(t)

	me := apiGet(t, "/me/Alice")
	missions := me["missions"].([]any)

	// Complete 3 missions to unlock bonus.
	completed := 0
	for _, raw := range missions {
		if completed >= 3 {
			break
		}
		m := raw.(map[string]any)
		if m["status"] == "active" {
			pmID := int(m["pm_id"].(float64))
			apiPost(t, "/complete", map[string]any{
				"player_name":  "Alice",
				"pm_id":        pmID,
				"participants": 1,
			})
			completed++
		}
	}

	// Claim bonus (normal = 2 extra missions).
	resp := apiPost(t, "/bonus", map[string]any{
		"player_name": "Alice",
		"choice":      "normal",
	})
	if resp["ok"] != true {
		t.Fatalf("bonus claim failed: %v", resp)
	}
}
