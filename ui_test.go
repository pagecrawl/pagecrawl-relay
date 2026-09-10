package main

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The settings page can set the enrolment token, so any page in the operator's
// browser that can reach it is a CSRF target. The random key is the primary
// defence; the Origin check is the second. A second defence that does not hold is
// worse than none, because it invites trusting it.
func newTestServer() *uiServer {
	return &uiServer{
		state: NewState(),
		cfg:   &Config{},
		key:   "test-key-0123456789",
		addr:  "127.0.0.1:54321",
	}
}

func TestOriginMustMatchExactly(t *testing.T) {
	s := newTestServer()

	allowed := []string{
		"http://127.0.0.1:54321",
		"http://localhost:54321",
		"http://[::1]:54321",
	}
	for _, origin := range allowed {
		if !s.originAllowed(origin) {
			t.Errorf("%s is where this page is served from and must be accepted", origin)
		}
	}

	// Each of these merely STARTS with an allowed value, which is what a prefix
	// check would wave through. All are domains an attacker can register.
	rejected := []string{
		"http://localhost.evil.com",
		"http://localhost:54321.evil.com",
		"http://127.0.0.1:54321.evil.com",
		"http://127.0.0.1:543210",
		"https://localhost:54321", // scheme must match too
		"http://localhost:1234",   // a different port is a different origin
		"http://evil.com",
		"null",
		"",
	}
	for _, origin := range rejected {
		if s.originAllowed(origin) {
			t.Errorf("%s must be refused", origin)
		}
	}
}

func TestMutationNeedsTheKey(t *testing.T) {
	s := newTestServer()

	req := httptest.NewRequest(http.MethodPost, "/api/token", nil)
	if s.authorised(req) {
		t.Fatal("a request with no key must be refused")
	}

	req.Header.Set("X-Relay-Key", "wrong")
	if s.authorised(req) {
		t.Fatal("a request with the wrong key must be refused")
	}

	req.Header.Set("X-Relay-Key", s.key)
	if !s.authorised(req) {
		t.Fatal("the correct key must be accepted")
	}
}

// Belt and braces: the right key from the wrong origin is still refused, which is
// the case that matters if a key ever leaks into a page that should not have it.
func TestCorrectKeyFromAForeignOriginIsRefused(t *testing.T) {
	s := newTestServer()

	req := httptest.NewRequest(http.MethodPost, "/api/token", nil)
	req.Header.Set("X-Relay-Key", s.key)
	req.Header.Set("Origin", "http://localhost.evil.com")

	if s.authorised(req) {
		t.Fatal("a foreign origin must be refused even with the correct key")
	}
}

// The settings page must survive a reload.
//
// The key arrives once in the URL and is stripped from the address bar, so a page
// that reads it only from location.search is authorised for exactly one page view.
// Reloading left every call answering 403, and the failure was swallowed, so the
// window rendered as blank with dashes for every value rather than saying anything.
func TestSettingsPageKeepsItsKeyAcrossAReload(t *testing.T) {
	if !strings.Contains(settingsPage, "sessionStorage") {
		t.Fatal("the page must keep the key for the tab, or a reload loses it")
	}

	if !strings.Contains(settingsPage, "history.replaceState") {
		t.Fatal("the key must still be dropped from the address bar")
	}
}

// A page that cannot read the state has to say so. This is the guard against the
// silent `.catch(() => {})` that made an unauthorised page look merely empty.
func TestSettingsPageReportsBeingLockedOut(t *testing.T) {
	for _, needle := range []string{"lockedMsg", "no longer authorised", "Cannot reach PageCrawl Relay"} {
		if !strings.Contains(settingsPage, needle) {
			t.Fatalf("the page must explain a failed state fetch, missing %q", needle)
		}
	}

	if strings.Contains(settingsPage, ".catch(() => {})") {
		t.Fatal("a swallowed error renders as a blank page with no explanation")
	}
}

// The page itself is served without a key (it has to be, since the browser follows a
// plain link), so every endpoint behind it must check.
func TestStateEndpointRefusesWithoutTheKey(t *testing.T) {
	srv := &uiServer{key: "correct-horse", state: NewState()}

	req := httptest.NewRequest(http.MethodGet, "/api/state", nil)
	if srv.authorised(req) {
		t.Fatal("a request with no key must not be authorised")
	}

	req.Header.Set("X-Relay-Key", "wrong")
	if srv.authorised(req) {
		t.Fatal("a request with the wrong key must not be authorised")
	}

	req.Header.Set("X-Relay-Key", "correct-horse")
	if !srv.authorised(req) {
		t.Fatal("the real key must be accepted")
	}
}

// A page kept open, bookmarked, or reopened from history has to keep working after
// the app restarts. With an ephemeral port it reached nothing and reported the relay
// as quit while it was running one port over.
func TestSettingsPageUsesAStablePort(t *testing.T) {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", preferredUIPort))
	if err != nil {
		t.Skipf("port %d busy on this machine, which is the case the fallback covers", preferredUIPort)
	}
	_ = ln.Close()

	url, err := startUI(NewState(), &Config{}, func(string) {}, func(bool) {})
	if err != nil {
		t.Fatalf("startUI: %v", err)
	}

	if !strings.Contains(url, fmt.Sprintf("127.0.0.1:%d/", preferredUIPort)) {
		t.Fatalf("expected the stable port in %q", url)
	}
}

// Two copies running at once must not fight over it.
func TestSettingsPageFallsBackWhenThePortIsTaken(t *testing.T) {
	blocker, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", preferredUIPort))
	if err != nil {
		t.Skipf("port %d already busy: %v", preferredUIPort, err)
	}
	defer blocker.Close()

	url, err := startUI(NewState(), &Config{}, func(string) {}, func(bool) {})
	if err != nil {
		t.Fatalf("startUI should fall back to any free port, got %v", err)
	}

	if strings.Contains(url, fmt.Sprintf(":%d/", preferredUIPort)) {
		t.Fatalf("expected a different port while %d is held, got %q", preferredUIPort, url)
	}
}

// Closing the settings page must not lock the operator out of their own relay.
//
// The key is minted per run and lives only in memory, so a fresh visit to the port is
// refused, correctly. Recording the URL is what makes `-open` able to get back in
// without restarting the relay and dropping every check in flight.
func TestTheSettingsURLIsRecordedForReopening(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir) // Linux
	t.Setenv("HOME", dir)            // macOS looks under HOME/Library

	const url = "http://127.0.0.1:28472/?k=deadbeef"

	rememberUIURL(url)

	if got := storedUIURL(); got != url {
		t.Fatalf("expected the URL back, got %q", got)
	}

	forgetUIURL()

	if got := storedUIURL(); got != "" {
		t.Fatalf("the record must go when the relay stops, got %q", got)
	}
}

// The page must be allowed to call its own API.
//
// This is the bug that made the settings page useless from the day it was written:
// "default-src 'none'" blocks fetch() even back to the page's own origin, so every
// call was refused by the browser before it left, and the page rendered with every
// card hidden and a dash in every value. It looked precisely like a relay that was
// not running, which sent two rounds of debugging after the wrong cause.
//
// curl cannot catch this, because curl does not enforce CSP. Nor can a test that only
// greps the HTML. The header is what matters, so the header is what is asserted.
func TestTheSettingsPageMayCallItsOwnAPI(t *testing.T) {
	srv := &uiServer{key: "k", state: NewState(), cfg: &Config{}}

	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	csp := rec.Header().Get("Content-Security-Policy")

	if csp == "" {
		t.Fatal("the settings page must still carry a Content-Security-Policy")
	}

	if !strings.Contains(csp, "connect-src 'self'") {
		t.Fatalf("CSP must allow same-origin fetch, or the page cannot read its own state: %q", csp)
	}

	// The rest of the policy is the point of having one: no remote anything.
	if !strings.Contains(csp, "default-src 'none'") {
		t.Fatalf("the page must still deny everything it does not need: %q", csp)
	}
}
