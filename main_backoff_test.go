package main

import (
	"errors"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// A rejected token must not be retried at full speed. The client used to redial
// with no pause at all after a connected-then-closed session, which against a bad
// token meant two complete connect and reject cycles per second, forever.
func TestRejectedTokenIsRecognised(t *testing.T) {
	for _, code := range []int{closeMissingToken, closeUnauthorized} {
		err := error(&websocket.CloseError{Code: code, Text: "Unauthorized"})
		if !isRejected(err) {
			t.Fatalf("close code %d should be treated as a rejection", code)
		}
	}
}

// Everything else is a transient network event and deserves a prompt retry.
func TestOrdinaryDisconnectsAreNotRejections(t *testing.T) {
	cases := []error{
		errors.New("read: connection reset by peer"),
		&websocket.CloseError{Code: websocket.CloseGoingAway, Text: "gateway restart"},
		&websocket.CloseError{Code: 4000, Text: "Replaced by a newer connection"},
		nil,
	}

	for _, err := range cases {
		if isRejected(err) {
			t.Fatalf("%v should not be treated as a rejection", err)
		}
	}
}

func TestBackoffGrowsAndIsCapped(t *testing.T) {
	max := 2 * time.Minute

	grown := growBackoff(time.Second, max)
	if grown <= time.Second {
		t.Fatalf("backoff should grow, got %s", grown)
	}

	// Jitter is added after the cap, so allow for it rather than asserting an
	// exact ceiling; the point is that it stays in the same order of magnitude.
	capped := growBackoff(10*time.Minute, max)
	if capped > max+time.Second {
		t.Fatalf("backoff should be capped near %s, got %s", max, capped)
	}
}
