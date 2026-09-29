package main

import (
	"strings"
	"testing"

	"github.com/pagecrawl/pagecrawl-relay/relay"
)

// The icon is the only status the menu bar shows, so each state it can be in has to
// pick a different one. A state that fell through to "offline" would quietly report
// a working relay as broken, or the reverse.
func TestEachStateChoosesItsOwnIcon(t *testing.T) {
	cases := []struct {
		name string
		snap relay.Snapshot
		want trayState
	}{
		{"relaying", relay.Snapshot{Connected: true}, iconRelaying},
		{"paused", relay.Snapshot{Paused: true}, iconPaused},
		{"not connected", relay.Snapshot{}, iconOffline},
		{"rejected token", relay.Snapshot{Rejected: true}, iconOffline},
		// Pausing is a decision the operator made, and it holds whether or not the
		// socket happens to still be up as the tunnel closes.
		{"paused while still connected", relay.Snapshot{Paused: true, Connected: true}, iconPaused},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := linesFor(c.snap).state; got != c.want {
				t.Fatalf("state = %v, want %v", got, c.want)
			}
		})
	}
}

// A token the gateway refuses is not a connection that will come back, so the menu
// has to say what to do about it rather than read as an ordinary disconnection.
func TestRejectedTokenSaysWhatToDo(t *testing.T) {
	status := linesFor(relay.Snapshot{Rejected: true}).status

	if !strings.Contains(status, "rejected") || !strings.Contains(status, "add this machine again") {
		t.Fatalf("status = %q, want it to name the rejection and the fix", status)
	}

	if plain := linesFor(relay.Snapshot{}).status; plain == status {
		t.Fatal("a rejected token reads the same as an ordinary disconnection")
	}
}

// The data carried left the menu bar; it did not leave the app.
func TestTrafficAndConnectionsStayInTheMenu(t *testing.T) {
	lines := linesFor(relay.Snapshot{Connected: true, Traffic: "4.2 MB", Connections: 3})

	if !strings.Contains(lines.traffic, "4.2 MB") || !strings.Contains(lines.traffic, "3") {
		t.Fatalf("traffic = %q, want the data carried and the connection count", lines.traffic)
	}
}

func TestUptimeReportsTheConnectionWhenThereIsOne(t *testing.T) {
	connected := linesFor(relay.Snapshot{Connected: true, ConnectedFor: "2 hours", Uptime: "3 hours"})
	if connected.uptime != "Connected for 2 hours" {
		t.Fatalf("uptime = %q", connected.uptime)
	}

	// Never connected: how long the app has been up is all there is to report.
	idle := linesFor(relay.Snapshot{Uptime: "3 hours"})
	if idle.uptime != "Running for 3 hours" {
		t.Fatalf("uptime = %q", idle.uptime)
	}
}

func TestPauseItemOffersTheOppositeAction(t *testing.T) {
	if got := linesFor(relay.Snapshot{Paused: true}).pause; got != "Resume relaying" {
		t.Fatalf("pause = %q", got)
	}

	if got := linesFor(relay.Snapshot{Connected: true}).pause; got != "Pause relaying" {
		t.Fatalf("pause = %q", got)
	}
}

func TestExitAddressAsksForACheckWhenUnknown(t *testing.T) {
	if got := linesFor(relay.Snapshot{ExitIP: "203.0.113.7"}).exit; got != "Your address: 203.0.113.7" {
		t.Fatalf("exit = %q", got)
	}

	// The menu has no self-check of its own, so the placeholder has to send people
	// to the one place that does rather than name an action that is not there.
	got := linesFor(relay.Snapshot{}).exit
	if !strings.Contains(got, "run a check") || !strings.Contains(got, "Settings") {
		t.Fatalf("exit = %q, want it to point at the settings page", got)
	}
}

// Every state needs its own tooltip: it is what someone reads when the icon alone
// leaves them unsure, so two states sharing one would defeat the point.
func TestTooltipsAreDistinct(t *testing.T) {
	seen := map[string]trayState{}

	for _, s := range []trayState{iconStarting, iconRelaying, iconPaused, iconOffline} {
		tip := s.tooltip()

		if !strings.HasPrefix(tip, "PageCrawl Relay") {
			t.Errorf("tooltip %q does not name the app", tip)
		}

		if other, dup := seen[tip]; dup {
			t.Errorf("states %v and %v share the tooltip %q", other, s, tip)
		}

		seen[tip] = s
	}
}
