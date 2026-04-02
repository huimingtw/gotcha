package e2e_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/playwright-community/playwright-go"
)

// resetGame calls POST /admin/reset to bring the server back to a clean state.
func resetGame(t *testing.T) {
	t.Helper()
	resp, err := http.Post(baseURL+"/admin/reset", "application/json", nil)
	if err != nil {
		t.Fatalf("resetGame: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("resetGame: unexpected status %d", resp.StatusCode)
	}
}

// openRoom calls POST /admin/open.
func openRoom(t *testing.T) {
	t.Helper()
	resp, err := http.Post(baseURL+"/admin/open", "application/json", nil)
	if err != nil {
		t.Fatalf("openRoom: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("openRoom: unexpected status %d", resp.StatusCode)
	}
}

// joinPlayer calls POST /join with the given name.
func joinPlayer(t *testing.T, name string) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"name": name})
	resp, err := http.Post(baseURL+"/join", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("joinPlayer(%q): %v", name, err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("joinPlayer(%q): unexpected status %d", name, resp.StatusCode)
	}
}

// startGame calls POST /start with a default duration.
func startGame(t *testing.T) {
	t.Helper()
	body, _ := json.Marshal(map[string]int{"duration_minutes": 120})
	resp, err := http.Post(baseURL+"/start", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("startGame: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("startGame: unexpected status %d", resp.StatusCode)
	}
}

// newPage creates a new browser page for a test and closes it on cleanup.
func newPage(t *testing.T) playwright.Page {
	t.Helper()
	page, err := browser.NewPage()
	if err != nil {
		t.Fatalf("newPage: %v", err)
	}
	t.Cleanup(func() { page.Close() })
	return page
}

// navigate goes to a URL and fails on error.
func navigate(t *testing.T, page playwright.Page, path string) {
	t.Helper()
	if _, err := page.Goto(baseURL + path); err != nil {
		t.Fatalf("navigate(%q): %v", path, err)
	}
}

// apiGet fetches a JSON endpoint and returns the decoded body.
func apiGet(t *testing.T, path string) map[string]any {
	t.Helper()
	resp, err := http.Get(baseURL + path)
	if err != nil {
		t.Fatalf("apiGet(%q): %v", path, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("apiGet(%q) decode: %v", path, err)
	}
	return out
}

// apiPost posts JSON and returns decoded response.
func apiPost(t *testing.T, path string, payload any) map[string]any {
	t.Helper()
	body, _ := json.Marshal(payload)
	resp, err := http.Post(baseURL+path, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("apiPost(%q): %v", path, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("apiPost(%q) decode: %v", path, err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("apiPost(%q): status %d body %v", path, resp.StatusCode, out)
	}
	return out
}

// mustContainText asserts that a locator's text contains the expected string.
func mustContainText(t *testing.T, page playwright.Page, selector, expected string) {
	t.Helper()
	loc := page.Locator(selector)
	if err := expect.Locator(loc).ToContainText(expected); err != nil {
		t.Fatalf("mustContainText(%q, %q): %v", selector, expected, err)
	}
}

// expect is the Playwright assertions helper, initialised once.
var expect = playwright.NewPlaywrightAssertions(5000)

// apiPostRaw posts JSON and returns only the HTTP status code.
func apiPostRaw(t *testing.T, path string, payload any) int {
	t.Helper()
	body, _ := json.Marshal(payload)
	resp, err := http.Post(baseURL+path, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("apiPostRaw(%q): %v", path, err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// unused import guard
var _ = fmt.Sprintf
