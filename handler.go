package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	qrcode "github.com/skip2/go-qrcode"
)

// ── SSE Hub ───────────────────────────────────────────────────────────────────

type sseClient struct {
	ch chan string
}

// SSEHub manages all connected SSE clients.
type SSEHub struct {
	mu      sync.Mutex
	clients map[*sseClient]bool
}

func newSSEHub() *SSEHub {
	return &SSEHub{clients: make(map[*sseClient]bool)}
}

// broadcast sends a JSON message to all connected clients.
func (h *SSEHub) broadcast(msg string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		select {
		case c.ch <- msg:
		default:
		}
	}
}

func (h *SSEHub) add(c *sseClient) {
	h.mu.Lock()
	h.clients[c] = true
	h.mu.Unlock()
}

func (h *SSEHub) remove(c *sseClient) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
}

// ── Handler ───────────────────────────────────────────────────────────────────

// Handler holds shared dependencies for all HTTP handlers.
type Handler struct {
	db  *DB
	hub *SSEHub

	mu            sync.Mutex
	autoEndCancel context.CancelFunc
}

func newHandler(db *DB) *Handler {
	return &Handler{
		db:  db,
		hub: newSSEHub(),
	}
}

// ── /events ───────────────────────────────────────────────────────────────────

func (h *Handler) Events(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")

	client := &sseClient{ch: make(chan string, 10)}
	h.hub.add(client)
	defer h.hub.remove(client)

	// Send current state immediately on connect.
	status, _ := h.db.GetState("status")
	endTime, _ := h.db.GetState("end_time")
	init := map[string]string{"type": "state", "status": status, "end_time": endTime}
	b, _ := json.Marshal(init)
	fmt.Fprintf(c.Writer, "data: %s\n\n", b)
	c.Writer.Flush()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	notify := c.Request.Context().Done()

	for {
		select {
		case <-notify:
			return
		case msg := <-client.ch:
			fmt.Fprintf(c.Writer, "data: %s\n\n", msg)
			c.Writer.Flush()
		case <-ticker.C:
			fmt.Fprintf(c.Writer, ": ping\n\n")
			c.Writer.Flush()
		}
	}
}

// ── /start ────────────────────────────────────────────────────────────────────

func (h *Handler) Start(c *gin.Context) {
	status, err := h.db.GetState("status")
	if err != nil || status != "waiting" {
		c.JSON(400, gin.H{"error": "請先開房，再開始遊戲"})
		return
	}

	var body struct {
		DurationMinutes int `json:"duration_minutes"`
	}
	_ = c.ShouldBindJSON(&body)
	if body.DurationMinutes <= 0 {
		body.DurationMinutes = 120
	}

	endTime := time.Now().Add(time.Duration(body.DurationMinutes) * time.Minute)
	if err := h.db.SetState("status", "playing"); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if err := h.db.SetState("end_time", endTime.UTC().Format(time.RFC3339)); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	// Collect player names first (close rows before DealCards — SQLite single-connection).
	names, err := h.db.ListPlayerNames()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	for _, name := range names {
		if err := h.db.DealCards(name); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
	}

	h.hub.broadcast(`{"type":"start"}`)
	h.scheduleAutoEnd(endTime)

	c.JSON(200, gin.H{"ok": true, "end_time": endTime.Format(time.RFC3339)})
}

// scheduleAutoEnd starts a goroutine that ends the game when the timer fires.
// Calling it again (e.g. after reset) cancels the previous goroutine.
func (h *Handler) scheduleAutoEnd(endTime time.Time) {
	ctx, cancel := context.WithCancel(context.Background())

	h.mu.Lock()
	if h.autoEndCancel != nil {
		h.autoEndCancel()
	}
	h.autoEndCancel = cancel
	h.mu.Unlock()

	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(endTime)):
		}
		status, err := h.db.GetState("status")
		if err != nil || status != "playing" {
			return
		}
		_ = h.db.SetState("status", "ended")
		h.hub.broadcast(`{"type":"end"}`)
	}()
}

// ── /join ─────────────────────────────────────────────────────────────────────

// validName accepts 1–20 Unicode letters, digits, or spaces.
var validName = regexp.MustCompile(`^[\p{L}\p{N} ]{1,20}$`)

func (h *Handler) Join(c *gin.Context) {
	var body struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Name == "" {
		c.JSON(400, gin.H{"error": "需要名字"})
		return
	}
	if !validName.MatchString(body.Name) {
		c.JSON(400, gin.H{"error": "名字只能包含文字、數字與空格，長度 1–20"})
		return
	}

	status, err := h.db.GetState("status")
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if status == "closed" {
		c.JSON(400, gin.H{"error": "房間尚未開放"})
		return
	}
	if status != "waiting" {
		// Allow rejoin for already-registered players.
		exists, err := h.db.PlayerExists(body.Name)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		if exists {
			c.JSON(200, gin.H{"ok": true})
			return
		}
		c.JSON(400, gin.H{"error": "遊戲已開始"})
		return
	}

	rows, err := h.db.InsertPlayer(body.Name)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if rows > 0 {
		h.hub.broadcast(fmt.Sprintf(`{"type":"join","player":"%s"}`, body.Name))
	}
	c.JSON(200, gin.H{"ok": true})
}

// ── /players ──────────────────────────────────────────────────────────────────

func (h *Handler) Players(c *gin.Context) {
	players, err := h.db.ListPlayers()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	status, _ := h.db.GetState("status")
	if players == nil {
		players = []Player{}
	}
	c.JSON(200, gin.H{"players": players, "status": status})
}

// ── /me/:name ─────────────────────────────────────────────────────────────────

func (h *Handler) Me(c *gin.Context) {
	name := c.Param("name")

	exists, err := h.db.PlayerExists(name)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if !exists {
		c.JSON(404, gin.H{"error": "玩家不存在"})
		return
	}

	missions, err := h.db.GetPlayerMissions(name)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if missions == nil {
		missions = []MissionRow{}
	}

	abilities, err := h.db.ListPlayerAbilities(name)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if abilities == nil {
		abilities = []AbilityRow{}
	}

	completedCount := 0
	for _, m := range missions {
		if m.Status == "completed" {
			completedCount++
		}
	}

	score, err := h.db.GetPlayerScore(name)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	gameStatus, _ := h.db.GetState("status")
	endTime, _ := h.db.GetState("end_time")

	c.JSON(200, gin.H{
		"name":            name,
		"missions":        missions,
		"abilities":       abilities,
		"score":           score,
		"completed_count": completedCount,
		"game_status":     gameStatus,
		"end_time":        endTime,
	})
}

// ── /complete ─────────────────────────────────────────────────────────────────

func (h *Handler) Complete(c *gin.Context) {
	var body struct {
		PlayerName   string `json:"player_name"`
		PMID         int    `json:"pm_id"`
		Participants int    `json:"participants"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "bad request"})
		return
	}

	status, err := h.db.GetState("status")
	if err != nil || status != "playing" {
		c.JSON(400, gin.H{"error": "遊戲未在進行"})
		return
	}

	m, err := h.db.GetMissionForPlayer(body.PMID, body.PlayerName)
	if err != nil {
		c.JSON(404, gin.H{"error": "任務不存在"})
		return
	}
	if m.CurrentStatus != "active" {
		c.JSON(400, gin.H{"error": "任務狀態不正確"})
		return
	}

	score := m.BaseScore
	if m.DoubleActive == 1 {
		score *= 2
	}

	if err := h.db.CompleteMission(body.PMID, score); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if err := h.db.InsertGameLog("complete", body.PlayerName,
		fmt.Sprintf("score:%d type:%s content:%s", score, m.MType, m.MContent)); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	completedCount, err := h.db.CountCompleted(body.PlayerName)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	evt, _ := json.Marshal(map[string]string{
		"type": "complete", "player": body.PlayerName,
		"content": m.MContent, "mtype": m.MType,
	})
	h.hub.broadcast(string(evt))

	resp := gin.H{"ok": true, "score_earned": score}
	if completedCount == 3 {
		normalAvail, _ := h.db.DrawFromPool("normal", body.PlayerName)
		hardAvail, _ := h.db.DrawFromPool("hard", body.PlayerName)
		resp["bonus_unlocked"] = true
		resp["bonus_normal_avail"] = normalAvail > 0
		resp["bonus_hard_avail"] = hardAvail > 0
	}
	c.JSON(200, resp)
}

// ── /fail ─────────────────────────────────────────────────────────────────────

func (h *Handler) Fail(c *gin.Context) {
	var body struct {
		AccuserName string `json:"accuser_name"`
		PMID        int    `json:"pm_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "bad request"})
		return
	}

	currentStatus, victimName, mContent, err := h.db.GetMissionStatus(body.PMID)
	if err != nil {
		c.JSON(404, gin.H{"error": "任務不存在"})
		return
	}
	if currentStatus != "active" {
		c.JSON(400, gin.H{"error": "任務狀態不正確"})
		return
	}

	if err := h.db.FailMission(body.PMID); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if err := h.db.InsertGameLog("accuse", body.AccuserName,
		fmt.Sprintf("victim:%s content:%s", victimName, mContent)); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	evt, _ := json.Marshal(map[string]string{
		"type": "accuse", "accuser": body.AccuserName,
		"victim": victimName, "content": mContent,
	})
	h.hub.broadcast(string(evt))
	c.JSON(200, gin.H{"ok": true})
}

// ── /ability ──────────────────────────────────────────────────────────────────

func (h *Handler) Ability(c *gin.Context) {
	var body struct {
		PlayerName string `json:"player_name"`
		Ability    string `json:"ability"`
		PMID       int    `json:"pm_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "bad request"})
		return
	}

	status, err := h.db.GetState("status")
	if err != nil || status != "playing" {
		c.JSON(400, gin.H{"error": "遊戲未在進行"})
		return
	}

	used, err := h.db.GetAbility(body.PlayerName, body.Ability)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(400, gin.H{"error": "能力卡已使用或不存在"})
		} else {
			c.JSON(500, gin.H{"error": err.Error()})
		}
		return
	}
	if used {
		c.JSON(400, gin.H{"error": "能力卡已使用或不存在"})
		return
	}

	pmStatus, err := h.db.GetPMStatus(body.PMID, body.PlayerName)
	if err != nil || pmStatus != "active" {
		c.JSON(400, gin.H{"error": "任務不存在或已結束"})
		return
	}

	switch body.Ability {
	case "swap":
		mType, err := h.db.GetMissionType(body.PMID)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		newID, err := h.db.DrawFromPool(mType, "")
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		if newID == 0 {
			c.JSON(400, gin.H{"error": "任務池已抽完，沒有更多任務可以換了"})
			return
		}
		if err := h.db.MarkAbilityUsed(body.PlayerName, body.Ability); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		if err := h.db.DiscardMission(body.PMID); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		if err := h.db.InsertPlayerMission(body.PlayerName, newID); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		if err := h.db.InsertGameLog("ability", body.PlayerName, "swap"); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		evt, _ := json.Marshal(map[string]string{"type": "ability", "player": body.PlayerName, "ability": "swap"})
		h.hub.broadcast(string(evt))
		c.JSON(200, gin.H{"ok": true, "action": "swap"})

	case "double":
		if err := h.db.MarkAbilityUsed(body.PlayerName, body.Ability); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		if err := h.db.SetDoubleActive(body.PMID); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		if err := h.db.InsertGameLog("ability", body.PlayerName, "double"); err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		evt, _ := json.Marshal(map[string]string{"type": "ability", "player": body.PlayerName, "ability": "double"})
		h.hub.broadcast(string(evt))
		c.JSON(200, gin.H{"ok": true, "action": "double"})

	default:
		c.JSON(400, gin.H{"error": "未知能力"})
	}
}

// ── /bonus ────────────────────────────────────────────────────────────────────

func (h *Handler) Bonus(c *gin.Context) {
	var body struct {
		PlayerName string `json:"player_name"`
		Choice     string `json:"choice"` // "normal" or "hard"
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "bad request"})
		return
	}

	status, err := h.db.GetState("status")
	if err != nil || status != "playing" {
		c.JSON(400, gin.H{"error": "遊戲未在進行"})
		return
	}

	completedCount, err := h.db.CountCompleted(body.PlayerName)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if completedCount < 3 {
		c.JSON(400, gin.H{"error": "尚未解鎖 Bonus"})
		return
	}

	claimed, err := h.db.BonusClaimed(body.PlayerName)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if claimed {
		c.JSON(400, gin.H{"error": "Bonus 已領取"})
		return
	}

	count := 1
	mType := "hard"
	if body.Choice == "normal" {
		count = 2
		mType = "normal"
	}

	for i := 0; i < count; i++ {
		newID, err := h.db.DrawFromPool(mType, body.PlayerName)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		if newID > 0 {
			if err := h.db.InsertPlayerMission(body.PlayerName, newID); err != nil {
				c.JSON(500, gin.H{"error": err.Error()})
				return
			}
		}
	}

	if err := h.db.InsertGameLog("bonus", body.PlayerName, fmt.Sprintf("choice:%s", body.Choice)); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

// ── /admin/open ───────────────────────────────────────────────────────────────

func (h *Handler) AdminOpen(c *gin.Context) {
	status, err := h.db.GetState("status")
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if status != "closed" {
		c.JSON(400, gin.H{"error": "房間已開放或遊戲已開始"})
		return
	}
	if err := h.db.SetState("status", "waiting"); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	h.hub.broadcast(`{"type":"open"}`)
	c.JSON(200, gin.H{"ok": true})
}

// ── /admin/end ────────────────────────────────────────────────────────────────

func (h *Handler) AdminEnd(c *gin.Context) {
	if err := h.db.SetState("status", "ended"); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	h.hub.broadcast(`{"type":"end"}`)
	c.JSON(200, gin.H{"ok": true})
}

// ── /admin/reset ──────────────────────────────────────────────────────────────

func (h *Handler) AdminReset(c *gin.Context) {
	// Cancel any running auto-end timer before resetting state.
	h.mu.Lock()
	if h.autoEndCancel != nil {
		h.autoEndCancel()
		h.autoEndCancel = nil
	}
	h.mu.Unlock()

	if err := h.db.ResetGame(); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	h.hub.broadcast(`{"type":"reset"}`)
	c.JSON(200, gin.H{"ok": true})
}

// ── /api/leaderboard ──────────────────────────────────────────────────────────

func (h *Handler) Leaderboard(c *gin.Context) {
	entries, err := h.db.GetLeaderboard()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	if entries == nil {
		entries = []LeaderboardEntry{}
	}

	status, _ := h.db.GetState("status")
	endTime, _ := h.db.GetState("end_time")

	resp := gin.H{
		"status":      status,
		"end_time":    endTime,
		"leaderboard": entries,
		"log":         []map[string]any{},
	}

	if status == "ended" {
		logEntries, err := h.db.ListGameLog()
		if err == nil {
			logSlice := make([]map[string]any, 0, len(logEntries))
			for _, e := range logEntries {
				logSlice = append(logSlice, map[string]any{
					"event_type":  e.EventType,
					"player_name": e.PlayerName,
					"detail":      e.Detail,
					"created_at":  e.CreatedAt,
				})
			}
			resp["log"] = logSlice
		}
	}
	c.JSON(200, resp)
}

// ── /game-log ─────────────────────────────────────────────────────────────────

func (h *Handler) GameLog(c *gin.Context) {
	entries, err := h.db.ListGameLog()
	if err != nil {
		c.JSON(200, gin.H{"log": []LogEntry{}})
		return
	}
	if entries == nil {
		entries = []LogEntry{}
	}
	c.JSON(200, gin.H{"log": entries})
}

// ── /qr.png ───────────────────────────────────────────────────────────────────

func (h *Handler) QRCode(c *gin.Context) {
	url := getNgrokURL()
	if url == "" {
		url = "http://localhost:8080"
	}
	png, err := qrcode.Encode(url, qrcode.Medium, 256)
	if err != nil {
		c.Status(500)
		return
	}
	c.Data(200, "image/png", png)
}

// ── /ngrok-url ────────────────────────────────────────────────────────────────

func (h *Handler) NgrokURL(c *gin.Context) {
	c.JSON(200, gin.H{"url": getNgrokURL()})
}

// ── ngrok helper ──────────────────────────────────────────────────────────────

func getNgrokURL() string {
	ngrokHost := os.Getenv("NGROK_API_HOST")
	if ngrokHost == "" {
		ngrokHost = "http://ngrok:4040"
	}
	resp, err := http.Get(ngrokHost + "/api/tunnels")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	var result struct {
		Tunnels []struct {
			PublicURL string `json:"public_url"`
			Proto     string `json:"proto"`
		} `json:"tunnels"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return ""
	}
	for _, t := range result.Tunnels {
		if t.Proto == "https" {
			return t.PublicURL
		}
	}
	if len(result.Tunnels) > 0 {
		return result.Tunnels[0].PublicURL
	}
	return ""
}
