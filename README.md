# GOTCHA

A social deduction party game for BBQ and hotpot gatherings. Players receive secret missions and try to complete them during the meal without getting caught. The host runs the server on a laptop; everyone joins via QR code on their phones.

## How it works

- Each player gets **4 normal missions** (2 pts each) and **1 hard mission** (3–7 pts)
- Complete missions secretly — if another player catches you, they can accuse and cancel it
- Two ability cards per player: **Swap** (replace a mission) and **Double** (double one mission's score)
- Complete 3 missions to unlock bonus cards
- Game ends when the timer runs out; scores reveal on the leaderboard

## Requirements

- [Docker](https://docs.docker.com/get-docker/) and Docker Compose
- A free [ngrok](https://ngrok.com) account with a static domain

## Quick start

```bash
git clone <this-repo>
cd gotcha

cp .env.example .env
# Edit .env: set NGROK_AUTHTOKEN and NGROK_DOMAIN

make docker-up
```

Open the admin panel at **http://localhost:8080/admin**, scan the QR code to share with players.

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `NGROK_AUTHTOKEN` | — | ngrok auth token (required for tunnel) |
| `NGROK_DOMAIN` | — | Your ngrok static domain |
| `DB_PATH` | `data/game.db` | SQLite file path |
| `PORT` | `8080` | HTTP port |

## Makefile commands

| Command | Description |
|---|---|
| `make run` | Run locally (Go 1.23+ required) |
| `make test` | Run all tests |
| `make docker-up` | Build and start app + ngrok |
| `make docker-down` | Stop containers |
| `make reset` | Wipe game data and restart |

## Admin panel

Visit `http://localhost:8080/admin` in your browser.

| Action | Description |
|---|---|
| **Open room** | Lets players join via the QR code |
| **Start game** | Deals mission cards and starts the timer |
| **End game** | Ends the game immediately and shows the leaderboard |
| **Reset** | Clears all players and game data for a new game |

## Local development

```bash
cp .env.example .env
make run
```

The server auto-initializes the SQLite database and seeds mission cards on first run. No manual SQL steps needed.

## Architecture

Three Go source files:

- `main.go` — wires dependencies and starts the server
- `handler.go` — all HTTP handlers and SSE hub
- `db.go` — all database access (SQLite via `modernc.org/sqlite`)

Frontend is plain HTML/JS served from `public/`. No build step required. Real-time updates use Server-Sent Events (SSE).
