package main

import (
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
	for _, needle := range []string{"lockedMsg", "no longer authorised", "Cannot reach the relay"} {
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
