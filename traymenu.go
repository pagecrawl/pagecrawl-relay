// What the menu-bar app shows for a given relay state. Kept out of tray.go, and so
// out of the `tray` build tag, because that file needs CGO and a Mac: the wording
// and the choice of icon are ordinary logic and are tested like any other.
package main

import (
	"fmt"

	"github.com/pagecrawl/pagecrawl-relay/relay"
)

// trayState is which icon the menu bar carries. The icon is the whole status
// display: there is no text beside it, so these four have to be distinguishable at
// a glance, and the menu spells the same state out in words.
type trayState int

const (
	iconStarting trayState = iota
	iconRelaying
	iconPaused
	iconOffline
)

// menuLines is everything the menu shows, in the order it appears.
type menuLines struct {
	state   trayState
	status  string
	uptime  string
	traffic string
	exit    string
	pause   string
}

func linesFor(s relay.Snapshot) menuLines {
	m := menuLines{}

	switch {
	case s.Paused:
		m.state, m.status = iconPaused, "Paused"
	case s.Connected:
		m.state, m.status = iconRelaying, "Connected"
	case s.Rejected:
		// Retrying will not fix a token the gateway refuses, and with no text in
		// the menu bar this line is the only place that can say so.
		m.state, m.status = iconOffline, "Token rejected: add this machine again"
	default:
		m.state, m.status = iconOffline, "Not connected"
	}

	if s.Connected && s.ConnectedFor != "" {
		m.uptime = "Connected for " + s.ConnectedFor
	} else {
		m.uptime = "Running for " + s.Uptime
	}

	m.traffic = fmt.Sprintf("%s carried, %d connections", s.Traffic, s.Connections)

	if s.ExitIP != "" {
		m.exit = "Your address: " + s.ExitIP
	} else {
		// Only the self-check asks an outside service what address it sees, and the
		// button that starts one is on the settings page, not in this menu.
		m.exit = "Your address: run a check in Settings"
	}

	if s.Paused {
		m.pause = "Resume relaying"
	} else {
		m.pause = "Pause relaying"
	}

	return m
}

func (s trayState) tooltip() string {
	switch s {
	case iconRelaying:
		return "PageCrawl Relay: relaying"
	case iconPaused:
		return "PageCrawl Relay: paused"
	case iconStarting:
		return "PageCrawl Relay: starting"
	default:
		return "PageCrawl Relay: not connected"
	}
}
