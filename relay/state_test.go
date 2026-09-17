package relay

import (
	"errors"
	"testing"

	"github.com/gorilla/websocket"
)

// A token the gateway refused stays refused, so the status has to say so plainly rather
// than leave a front end showing "retrying" forever.
func TestStatusReportsARejectedToken(t *testing.T) {
	state := NewState()

	state.MarkDisconnected(&websocket.CloseError{Code: closeUnauthorized, Text: "Unauthorized"})

	if !state.Snapshot().Rejected {
		t.Fatal("an unauthorized close was not reported as rejected")
	}
}

// An ordinary dropped link is worth retrying, and must not read as a bad key.
func TestStatusDoesNotCallADroppedLinkARejection(t *testing.T) {
	state := NewState()

	state.MarkDisconnected(errors.New("connection reset by peer"))

	if state.Snapshot().Rejected {
		t.Fatal("a dropped connection was reported as a rejected token")
	}
}

// After enrolling again the relay connects, and the old rejection must not linger.
func TestConnectingClearsAnEarlierRejection(t *testing.T) {
	state := NewState()
	state.MarkDisconnected(&websocket.CloseError{Code: closeMissingToken})

	state.MarkConnected("")

	if state.Snapshot().Rejected {
		t.Fatal("still reported as rejected after connecting")
	}
}
