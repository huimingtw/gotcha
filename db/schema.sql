CREATE TABLE IF NOT EXISTS game_state (
    key   TEXT PRIMARY KEY,
    value TEXT
);

CREATE TABLE IF NOT EXISTS players (
    name      TEXT PRIMARY KEY,
    joined_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS missions (
    id         INTEGER PRIMARY KEY,
    content    TEXT,
    type       TEXT,
    base_score INTEGER
);

CREATE TABLE IF NOT EXISTS player_missions (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    player_name  TEXT,
    mission_id   INTEGER,
    status       TEXT,
    completed_at TIMESTAMP,
    score_earned INTEGER,
    double_active INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS player_abilities (
    player_name TEXT,
    ability     TEXT,
    used        INTEGER DEFAULT 0
);

CREATE TABLE IF NOT EXISTS game_log (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    event_type  TEXT,
    player_name TEXT,
    detail      TEXT,
    created_at  TIMESTAMP
);
