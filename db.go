package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	_ "modernc.org/sqlite"
)

// DB wraps sql.DB and owns all game data-access operations.
type DB struct {
	db      *sql.DB
	logFile string // path to the flat events.log file
}

// ── Domain types ─────────────────────────────────────────────────────────────

// Player is a joined participant.
type Player struct {
	Name     string `json:"name"`
	JoinedAt string `json:"joined_at"`
}

// MissionRow is a player_mission row joined with the mission definition.
type MissionRow struct {
	PMID         int    `json:"pm_id"`
	ID           int    `json:"id"`
	Content      string `json:"content"`
	Type         string `json:"type"`
	BaseScore    int    `json:"base_score"`
	Status       string `json:"status"`
	DoubleActive bool   `json:"double_active"`
	ScoreEarned  int    `json:"score_earned"`
}

// AbilityRow is a player's ability card.
type AbilityRow struct {
	Name string `json:"name"`
	Used bool   `json:"used"`
}

// LogEntry is a single game event record.
type LogEntry struct {
	EventType  string `json:"event_type"`
	PlayerName string `json:"player_name"`
	Detail     string `json:"detail"`
	CreatedAt  string `json:"created_at"`
}

// LeaderboardEntry is a player's final standing.
type LeaderboardEntry struct {
	Name      string `json:"name"`
	Score     int    `json:"score"`
	Completed int    `json:"completed"`
}

// missionLookup is an internal result for GetMissionForPlayer.
type missionLookup struct {
	MType         string
	MContent      string
	BaseScore     int
	DoubleActive  int
	CurrentStatus string
}

// ── Lifecycle ────────────────────────────────────────────────────────────────

// Open opens (or creates) the SQLite DB at path, applies the schema, and seeds missions.
func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("mkdir: %w", err)
	}
	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)

	schemaPath := os.Getenv("SCHEMA_PATH")
	if schemaPath == "" {
		schemaPath = "db/schema.sql"
	}
	schemaBytes, err := os.ReadFile(schemaPath)
	if err != nil {
		return nil, fmt.Errorf("read schema: %w", err)
	}
	if _, err := sqlDB.Exec(string(schemaBytes)); err != nil {
		return nil, fmt.Errorf("exec schema: %w", err)
	}

	logFile := filepath.Join(filepath.Dir(path), "events.log")
	d := &DB{db: sqlDB, logFile: logFile}

	// Bootstrap game_state rows.
	if _, err := sqlDB.Exec(`INSERT OR IGNORE INTO game_state VALUES ('status','closed')`); err != nil {
		return nil, fmt.Errorf("seed status: %w", err)
	}
	if _, err := sqlDB.Exec(`INSERT OR IGNORE INTO game_state VALUES ('end_time','')`); err != nil {
		return nil, fmt.Errorf("seed end_time: %w", err)
	}
	// If server restarted mid-game, roll back to closed (keep 'waiting' intact).
	if _, err := sqlDB.Exec(`UPDATE game_state SET value='closed' WHERE key='status' AND value='playing'`); err != nil {
		return nil, fmt.Errorf("reset playing state: %w", err)
	}

	if err := d.seedMissions(); err != nil {
		return nil, fmt.Errorf("seed missions: %w", err)
	}
	return d, nil
}

// Close closes the underlying connection.
func (d *DB) Close() error {
	return d.db.Close()
}

// ── Game state ───────────────────────────────────────────────────────────────

// GetState returns the value for a game_state key.
func (d *DB) GetState(key string) (string, error) {
	var val string
	err := d.db.QueryRow(`SELECT value FROM game_state WHERE key=?`, key).Scan(&val)
	if err != nil {
		return "", fmt.Errorf("GetState %q: %w", key, err)
	}
	return val, nil
}

// SetState upserts a game_state key/value pair.
func (d *DB) SetState(key, val string) error {
	_, err := d.db.Exec(`INSERT OR REPLACE INTO game_state VALUES (?,?)`, key, val)
	if err != nil {
		return fmt.Errorf("SetState %q: %w", key, err)
	}
	return nil
}

// ── Players ──────────────────────────────────────────────────────────────────

// InsertPlayer adds a new player. Returns rows affected (0 if name already exists).
func (d *DB) InsertPlayer(name string) (int64, error) {
	result, err := d.db.Exec(
		`INSERT OR IGNORE INTO players (name, joined_at) VALUES (?,?)`,
		name, time.Now().UTC(),
	)
	if err != nil {
		return 0, fmt.Errorf("InsertPlayer: %w", err)
	}
	rows, _ := result.RowsAffected()
	return rows, nil
}

// PlayerExists checks whether a name is registered.
func (d *DB) PlayerExists(name string) (bool, error) {
	var exists bool
	err := d.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM players WHERE name=?)`, name).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("PlayerExists: %w", err)
	}
	return exists, nil
}

// ListPlayers returns all players ordered by join time.
func (d *DB) ListPlayers() ([]Player, error) {
	rows, err := d.db.Query(`SELECT name, joined_at FROM players ORDER BY joined_at`)
	if err != nil {
		return nil, fmt.Errorf("ListPlayers: %w", err)
	}
	defer rows.Close()
	var players []Player
	for rows.Next() {
		var p Player
		if err := rows.Scan(&p.Name, &p.JoinedAt); err != nil {
			return nil, fmt.Errorf("ListPlayers scan: %w", err)
		}
		players = append(players, p)
	}
	return players, rows.Err()
}

// ListPlayerNames returns only the name column for all players.
func (d *DB) ListPlayerNames() ([]string, error) {
	rows, err := d.db.Query(`SELECT name FROM players`)
	if err != nil {
		return nil, fmt.Errorf("ListPlayerNames: %w", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("ListPlayerNames scan: %w", err)
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// ── Card dealing ─────────────────────────────────────────────────────────────

// DealCards assigns 4 normal + 1 hard mission + 2 ability cards to a player.
// All rows are fully consumed before any Exec to satisfy the SQLite single-connection constraint.
func (d *DB) DealCards(playerName string) error {
	// Collect normal IDs first, close rows before any Exec.
	normalRows, err := d.db.Query(`
		SELECT id FROM missions WHERE type='normal'
		AND id NOT IN (SELECT mission_id FROM player_missions WHERE status IN ('active','completed'))
		ORDER BY RANDOM() LIMIT 4`)
	if err != nil {
		return fmt.Errorf("DealCards query normal: %w", err)
	}
	var normalIDs []int
	for normalRows.Next() {
		var id int
		if err := normalRows.Scan(&id); err != nil {
			normalRows.Close()
			return fmt.Errorf("DealCards scan normal: %w", err)
		}
		normalIDs = append(normalIDs, id)
	}
	normalRows.Close()
	if err := normalRows.Err(); err != nil {
		return fmt.Errorf("DealCards normal rows: %w", err)
	}

	for _, id := range normalIDs {
		if _, err := d.db.Exec(
			`INSERT INTO player_missions (player_name, mission_id, status) VALUES (?,?,'active')`,
			playerName, id,
		); err != nil {
			return fmt.Errorf("DealCards insert normal: %w", err)
		}
	}

	// Hard mission.
	var hardID int
	err = d.db.QueryRow(`
		SELECT id FROM missions WHERE type='hard'
		AND id NOT IN (SELECT mission_id FROM player_missions WHERE status IN ('active','completed'))
		ORDER BY RANDOM() LIMIT 1`).Scan(&hardID)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("DealCards query hard: %w", err)
	}
	if hardID > 0 {
		if _, err := d.db.Exec(
			`INSERT INTO player_missions (player_name, mission_id, status) VALUES (?,?,'active')`,
			playerName, hardID,
		); err != nil {
			return fmt.Errorf("DealCards insert hard: %w", err)
		}
	}

	// Ability cards.
	for _, ability := range []string{"swap", "double"} {
		if _, err := d.db.Exec(
			`INSERT OR IGNORE INTO player_abilities VALUES (?,?,0)`,
			playerName, ability,
		); err != nil {
			return fmt.Errorf("DealCards insert ability %q: %w", ability, err)
		}
	}
	return nil
}

// ── Missions ─────────────────────────────────────────────────────────────────

// GetPlayerMissions returns all missions for a player ordered by pm.id.
func (d *DB) GetPlayerMissions(playerName string) ([]MissionRow, error) {
	rows, err := d.db.Query(`
		SELECT pm.id, m.id, m.content, m.type, m.base_score,
		       pm.status, pm.double_active, COALESCE(pm.score_earned,0)
		FROM player_missions pm
		JOIN missions m ON pm.mission_id = m.id
		WHERE pm.player_name=?
		ORDER BY pm.id`, playerName)
	if err != nil {
		return nil, fmt.Errorf("GetPlayerMissions: %w", err)
	}
	defer rows.Close()
	var missions []MissionRow
	for rows.Next() {
		var m MissionRow
		var da int
		if err := rows.Scan(&m.PMID, &m.ID, &m.Content, &m.Type, &m.BaseScore, &m.Status, &da, &m.ScoreEarned); err != nil {
			return nil, fmt.Errorf("GetPlayerMissions scan: %w", err)
		}
		m.DoubleActive = da == 1
		missions = append(missions, m)
	}
	return missions, rows.Err()
}

// GetMissionForPlayer fetches mission details by pm_id + player_name (for complete/ability).
func (d *DB) GetMissionForPlayer(pmID int, playerName string) (missionLookup, error) {
	var m missionLookup
	err := d.db.QueryRow(`
		SELECT m.type, m.content, m.base_score, pm.double_active, pm.status
		FROM player_missions pm
		JOIN missions m ON pm.mission_id = m.id
		WHERE pm.id=? AND pm.player_name=?`, pmID, playerName).
		Scan(&m.MType, &m.MContent, &m.BaseScore, &m.DoubleActive, &m.CurrentStatus)
	if err != nil {
		return missionLookup{}, fmt.Errorf("GetMissionForPlayer: %w", err)
	}
	return m, nil
}

// GetMissionStatus fetches pm status, owner name, and content by pm_id only (for fail/accuse).
func (d *DB) GetMissionStatus(pmID int) (status, playerName, content string, err error) {
	err = d.db.QueryRow(`
		SELECT pm.status, pm.player_name, m.content
		FROM player_missions pm JOIN missions m ON pm.mission_id = m.id
		WHERE pm.id=?`, pmID).Scan(&status, &playerName, &content)
	if err != nil {
		return "", "", "", fmt.Errorf("GetMissionStatus: %w", err)
	}
	return status, playerName, content, nil
}

// GetMissionType returns the type ('normal'/'hard') of a player_mission.
func (d *DB) GetMissionType(pmID int) (string, error) {
	var mType string
	err := d.db.QueryRow(
		`SELECT m.type FROM player_missions pm JOIN missions m ON pm.mission_id=m.id WHERE pm.id=?`,
		pmID,
	).Scan(&mType)
	if err != nil {
		return "", fmt.Errorf("GetMissionType: %w", err)
	}
	return mType, nil
}

// GetPMStatus returns the status of a player_mission (scoped to playerName).
func (d *DB) GetPMStatus(pmID int, playerName string) (string, error) {
	var status string
	err := d.db.QueryRow(
		`SELECT status FROM player_missions WHERE id=? AND player_name=?`,
		pmID, playerName,
	).Scan(&status)
	if err != nil {
		return "", fmt.Errorf("GetPMStatus: %w", err)
	}
	return status, nil
}

// CompleteMission marks a pm as completed with the given score.
func (d *DB) CompleteMission(pmID, score int) error {
	_, err := d.db.Exec(
		`UPDATE player_missions SET status='completed', completed_at=?, score_earned=? WHERE id=?`,
		time.Now().UTC(), score, pmID,
	)
	if err != nil {
		return fmt.Errorf("CompleteMission: %w", err)
	}
	return nil
}

// FailMission marks a pm as failed.
func (d *DB) FailMission(pmID int) error {
	_, err := d.db.Exec(`UPDATE player_missions SET status='failed' WHERE id=?`, pmID)
	if err != nil {
		return fmt.Errorf("FailMission: %w", err)
	}
	return nil
}

// DiscardMission marks a pm as discarded (swap ability).
func (d *DB) DiscardMission(pmID int) error {
	_, err := d.db.Exec(`UPDATE player_missions SET status='discarded' WHERE id=?`, pmID)
	if err != nil {
		return fmt.Errorf("DiscardMission: %w", err)
	}
	return nil
}

// InsertPlayerMission adds a new active mission for a player.
func (d *DB) InsertPlayerMission(playerName string, missionID int) error {
	_, err := d.db.Exec(
		`INSERT INTO player_missions (player_name, mission_id, status) VALUES (?,?,'active')`,
		playerName, missionID,
	)
	if err != nil {
		return fmt.Errorf("InsertPlayerMission: %w", err)
	}
	return nil
}

// SetDoubleActive activates the double-score flag on a pm.
func (d *DB) SetDoubleActive(pmID int) error {
	_, err := d.db.Exec(`UPDATE player_missions SET double_active=1 WHERE id=?`, pmID)
	if err != nil {
		return fmt.Errorf("SetDoubleActive: %w", err)
	}
	return nil
}

// ── Abilities ────────────────────────────────────────────────────────────────

// GetAbility returns whether a player's ability card has been used.
// Returns sql.ErrNoRows wrapped in the error if the ability does not exist.
func (d *DB) GetAbility(playerName, ability string) (used bool, err error) {
	var usedInt int
	err = d.db.QueryRow(
		`SELECT used FROM player_abilities WHERE player_name=? AND ability=?`,
		playerName, ability,
	).Scan(&usedInt)
	if err != nil {
		return false, fmt.Errorf("GetAbility: %w", err)
	}
	return usedInt == 1, nil
}

// MarkAbilityUsed sets used=1 for a player's ability.
func (d *DB) MarkAbilityUsed(playerName, ability string) error {
	_, err := d.db.Exec(
		`UPDATE player_abilities SET used=1 WHERE player_name=? AND ability=?`,
		playerName, ability,
	)
	if err != nil {
		return fmt.Errorf("MarkAbilityUsed: %w", err)
	}
	return nil
}

// ListPlayerAbilities returns all ability cards for a player.
func (d *DB) ListPlayerAbilities(playerName string) ([]AbilityRow, error) {
	rows, err := d.db.Query(
		`SELECT ability, used FROM player_abilities WHERE player_name=?`,
		playerName,
	)
	if err != nil {
		return nil, fmt.Errorf("ListPlayerAbilities: %w", err)
	}
	defer rows.Close()
	var abilities []AbilityRow
	for rows.Next() {
		var a AbilityRow
		var used int
		if err := rows.Scan(&a.Name, &used); err != nil {
			return nil, fmt.Errorf("ListPlayerAbilities scan: %w", err)
		}
		a.Used = used == 1
		abilities = append(abilities, a)
	}
	return abilities, rows.Err()
}

// ── Mission pool ─────────────────────────────────────────────────────────────

// DrawFromPool draws one random mission of mType not already active/completed.
// When excludePlayer is non-empty, also excludes missions assigned to that player.
// Returns 0 (not an error) if the pool is empty.
func (d *DB) DrawFromPool(mType, excludePlayer string) (int, error) {
	var id int
	var err error
	if excludePlayer != "" {
		err = d.db.QueryRow(`
			SELECT id FROM missions WHERE type=?
			AND id NOT IN (
				SELECT mission_id FROM player_missions
				WHERE status IN ('active','completed') AND player_name=?
			)
			ORDER BY RANDOM() LIMIT 1`, mType, excludePlayer).Scan(&id)
	} else {
		err = d.db.QueryRow(`
			SELECT id FROM missions WHERE type=?
			AND id NOT IN (
				SELECT mission_id FROM player_missions WHERE status IN ('active','completed')
			)
			ORDER BY RANDOM() LIMIT 1`, mType).Scan(&id)
	}
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("DrawFromPool: %w", err)
	}
	return id, nil
}

// ── Scoring ──────────────────────────────────────────────────────────────────

// GetPlayerScore returns the total score for a player.
func (d *DB) GetPlayerScore(playerName string) (int, error) {
	var score int
	err := d.db.QueryRow(
		`SELECT COALESCE(SUM(score_earned),0) FROM player_missions WHERE player_name=? AND status='completed'`,
		playerName,
	).Scan(&score)
	if err != nil {
		return 0, fmt.Errorf("GetPlayerScore: %w", err)
	}
	return score, nil
}

// CountCompleted returns the number of completed missions for a player.
func (d *DB) CountCompleted(playerName string) (int, error) {
	var count int
	err := d.db.QueryRow(
		`SELECT COUNT(*) FROM player_missions WHERE player_name=? AND status='completed'`,
		playerName,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("CountCompleted: %w", err)
	}
	return count, nil
}

// BonusClaimed returns true if the player has already claimed the bonus mission.
func (d *DB) BonusClaimed(playerName string) (bool, error) {
	var count int
	err := d.db.QueryRow(
		`SELECT COUNT(*) FROM game_log WHERE event_type='bonus' AND player_name=?`,
		playerName,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("BonusClaimed: %w", err)
	}
	return count > 0, nil
}

// ── Leaderboard ──────────────────────────────────────────────────────────────

// GetLeaderboard returns all players sorted by score descending.
// Names are collected first (rows closed) before further queries — SQLite single-connection constraint.
func (d *DB) GetLeaderboard() ([]LeaderboardEntry, error) {
	names, err := d.ListPlayerNames()
	if err != nil {
		return nil, fmt.Errorf("GetLeaderboard names: %w", err)
	}
	entries := make([]LeaderboardEntry, 0, len(names))
	for _, name := range names {
		score, err := d.GetPlayerScore(name)
		if err != nil {
			return nil, err
		}
		completed, err := d.CountCompleted(name)
		if err != nil {
			return nil, err
		}
		entries = append(entries, LeaderboardEntry{Name: name, Score: score, Completed: completed})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Score > entries[j].Score
	})
	return entries, nil
}

// ── Game log ─────────────────────────────────────────────────────────────────

// InsertGameLog records a game event in the DB and appends to the flat events.log file.
func (d *DB) InsertGameLog(eventType, playerName, detail string) error {
	now := time.Now()
	_, err := d.db.Exec(
		`INSERT INTO game_log (event_type, player_name, detail, created_at) VALUES (?,?,?,?)`,
		eventType, playerName, detail, now.UTC(),
	)
	if err != nil {
		return fmt.Errorf("InsertGameLog: %w", err)
	}
	// Append to flat file (best-effort — don't fail on write error).
	line := fmt.Sprintf("[%s] %s | %s | %s\n",
		now.Local().Format("2006-01-02 15:04:05"), eventType, playerName, detail)
	if f, err := os.OpenFile(d.logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644); err == nil {
		_, _ = f.WriteString(line)
		f.Close()
	}
	return nil
}

// ListGameLog returns all game log entries ordered by time.
func (d *DB) ListGameLog() ([]LogEntry, error) {
	rows, err := d.db.Query(
		`SELECT event_type, player_name, detail, created_at FROM game_log ORDER BY created_at`,
	)
	if err != nil {
		return nil, fmt.Errorf("ListGameLog: %w", err)
	}
	defer rows.Close()
	var entries []LogEntry
	for rows.Next() {
		var e LogEntry
		if err := rows.Scan(&e.EventType, &e.PlayerName, &e.Detail, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("ListGameLog scan: %w", err)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// ── Reset ────────────────────────────────────────────────────────────────────

// ResetGame clears all player/mission data inside a transaction, then re-seeds missions.
func (d *DB) ResetGame() error {
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("ResetGame begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	for _, stmt := range []string{
		`DELETE FROM players`,
		`DELETE FROM player_missions`,
		`DELETE FROM player_abilities`,
		`DELETE FROM game_log`,
		`DELETE FROM missions`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("ResetGame %q: %w", stmt, err)
		}
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO game_state VALUES ('status','closed')`); err != nil {
		return fmt.Errorf("ResetGame status: %w", err)
	}
	if _, err := tx.Exec(`INSERT OR REPLACE INTO game_state VALUES ('end_time','')`); err != nil {
		return fmt.Errorf("ResetGame end_time: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("ResetGame commit: %w", err)
	}

	// Re-seed missions after the transaction (seedMissions issues its own Exec).
	return d.seedMissions()
}

// ── Seed ─────────────────────────────────────────────────────────────────────

// seedMissions inserts all missions from db/seed.sql if the table is empty.
func (d *DB) seedMissions() error {
	var count int
	if err := d.db.QueryRow(`SELECT COUNT(*) FROM missions`).Scan(&count); err != nil {
		return fmt.Errorf("count missions: %w", err)
	}
	if count > 0 {
		return nil
	}
	seedPath := os.Getenv("SEED_PATH")
	if seedPath == "" {
		seedPath = "db/seed.sql"
	}
	seedBytes, err := os.ReadFile(seedPath)
	if err != nil {
		return fmt.Errorf("read seed: %w", err)
	}
	if _, err := d.db.Exec(string(seedBytes)); err != nil {
		return fmt.Errorf("exec seed: %w", err)
	}
	return nil
}
