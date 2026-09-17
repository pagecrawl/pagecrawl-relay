// Package relay is the PageCrawl Relay client: the tunnel to the relay gateway, the
// destination guard, and the loop that keeps the tunnel up.
//
// It has no user interface and no idea where its settings live. The desktop program
// (the pagecrawl-relay binary at the module root) wraps it with a settings page, a tray
// icon and a config file. Keeping it separate means any other front end runs exactly this
// code, so a fix to the guard or the protocol reaches every platform.
package relay

import "time"

// DefaultGateway is the production relay gateway.
const DefaultGateway = "wss://relay.pagecrawl.io/tunnel"

type Config struct {
	GatewayURL  string
	Token       string
	Verbose     bool
	DialTimeout time.Duration
	IdleTimeout time.Duration
	MaxBackoff  time.Duration

	// Reported to the gateway on connect. Supplied by the caller rather than read here:
	// the desktop binary stamps its version at build time with -X main.Version, which a
	// library cannot see.
	Platform string
	Version  string
}

// DefaultConfig is the timing every client uses unless it has a reason not to.
func DefaultConfig() Config {
	return Config{
		GatewayURL:  DefaultGateway,
		DialTimeout: 15 * time.Second,
		// Longer than any single page load, shorter than the worker's own per-attempt
		// cap, so a stalled stream is reclaimed before the check gives up.
		IdleTimeout: 120 * time.Second,
		MaxBackoff:  2 * time.Minute,
	}
}

// Store keeps the settings a person changes while the relay runs, so they survive a
// restart. The desktop program writes them to its config file. A setting change is only
// reported as saved when Store says it was, so a relay never claims a change it lost.
type Store interface {
	SaveToken(token string) error
	SavePaused(paused bool) error
}
