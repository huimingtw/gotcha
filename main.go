package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
)

func main() {
	// ── Open DB ──────────────────────────────────────────────────────────────
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "data/game.db"
	}
	database, err := Open(dbPath)
	if err != nil {
		slog.Error("open db", "err", err)
		os.Exit(1)
	}

	// ── Wire handlers ────────────────────────────────────────────────────────
	h := newHandler(database)

	// ── Router ───────────────────────────────────────────────────────────────
	if os.Getenv("GIN_MODE") == "" {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.Default()

	// Player endpoints
	r.POST("/join", h.Join)
	r.GET("/players", h.Players)
	r.GET("/me/:name", h.Me)

	// Game flow
	r.GET("/events", h.Events)
	r.POST("/start", h.Start)

	// Mission actions
	r.POST("/complete", h.Complete)
	r.POST("/fail", h.Fail)
	r.POST("/ability", h.Ability)
	r.POST("/bonus", h.Bonus)

	// Public read endpoints
	r.GET("/api/leaderboard", h.Leaderboard)
	r.GET("/game-log", h.GameLog)
	r.GET("/qr.png", h.QRCode)
	r.GET("/ngrok-url", h.NgrokURL)

	// Admin endpoints
	admin := r.Group("/admin")
	admin.POST("/open", h.AdminOpen)
	admin.POST("/end", h.AdminEnd)
	admin.POST("/reset", h.AdminReset)

	// Static files
	r.StaticFile("/style.css", "public/style.css")
	r.StaticFile("/shared.js", "public/shared.js")
	r.StaticFile("/claw.svg", "public/claw.svg")
	r.GET("/", func(c *gin.Context) { c.File("public/index.html") })
	r.GET("/closed", func(c *gin.Context) { c.File("public/closed.html") })
	r.GET("/waiting", func(c *gin.Context) { c.File("public/waiting.html") })
	r.GET("/game", func(c *gin.Context) { c.File("public/game.html") })
	r.GET("/leaderboard", func(c *gin.Context) { c.File("public/leaderboard.html") })
	r.GET("/admin", func(c *gin.Context) { c.File("public/admin.html") })

	// ── Graceful shutdown ────────────────────────────────────────────────────
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	srv := &http.Server{Addr: ":" + port, Handler: r}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("listening", "port", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("listen", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Warn("shutdown error", "err", err)
	}
	if err := database.Close(); err != nil {
		slog.Warn("db close error", "err", err)
	}
	slog.Info("bye")
}
