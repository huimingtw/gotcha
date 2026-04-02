package e2e_test

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/playwright-community/playwright-go"
)

var (
	pw      *playwright.Playwright
	browser playwright.Browser
	baseURL = "http://localhost:18080"
)

func TestMain(m *testing.M) {
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	// Install Playwright browsers if needed.
	if err := playwright.Install(); err != nil {
		fmt.Fprintf(os.Stderr, "playwright install: %v\n", err)
		return 1
	}

	// Start the game server.
	srv := exec.Command("go", "run", ".")
	srv.Dir = ".."
	srv.Env = append(os.Environ(),
		"PORT=18080",
		"DB_PATH=/tmp/gotcha_e2e_test.db",
	)
	srv.Stdout = os.Stdout
	srv.Stderr = os.Stderr
	if err := srv.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "start server: %v\n", err)
		return 1
	}
	defer srv.Process.Kill() //nolint:errcheck

	// Wait for server to be ready (up to 30s).
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/")
		if err == nil {
			resp.Body.Close()
			break
		}
		time.Sleep(300 * time.Millisecond)
	}

	// Init Playwright.
	var err error
	pw, err = playwright.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "playwright run: %v\n", err)
		return 1
	}
	defer pw.Stop() //nolint:errcheck

	browser, err = pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(true),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "launch browser: %v\n", err)
		return 1
	}
	defer browser.Close()

	return m.Run()
}
