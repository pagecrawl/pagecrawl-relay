package main

import (
	"testing"
	"time"
)

// The keepalive constants exist to survive the shortest timeout on the path, so
// pin the relationships rather than leaving them as three unexplained numbers.
func TestKeepaliveTimings(t *testing.T) {
	// Keep pings well within a 100-second idle timeout to leave room for delays
	// from gateways, home routers and corporate firewalls.
	const idleCutoff = 100 * time.Second

	if pingInterval >= idleCutoff/2 {
		t.Errorf("pingInterval %s leaves no margin under the %s idle cutoff", pingInterval, idleCutoff)
	}

	// One lost pong must not tear down a healthy tunnel.
	if pongWait <= pingInterval*2 {
		t.Errorf("pongWait %s must exceed two ping intervals (%s)", pongWait, pingInterval*2)
	}

	// A write that blocks longer than the ping cadence would stack up pings.
	if writeWait >= pingInterval {
		t.Errorf("writeWait %s must be shorter than pingInterval %s", writeWait, pingInterval)
	}
}
