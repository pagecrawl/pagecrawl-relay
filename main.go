// PageCrawl Relay client.
//
// Runs on a machine the customer owns and carries the network egress for THEIR OWN
// PageCrawl monitors. It dials out to the relay gateway, so it needs no port
// forwarding and no inbound firewall rule, and it only ever moves bytes for checks
// the gateway has already authorised against this machine's team.
//
// It is not a general-purpose proxy: it opens no listener for other traffic, and
// every destination is re-validated here (see guard.go) before a single byte moves.
//
// Three ways to run it, in the order most people need them:
//
//	pagecrawl-relay             desktop: opens a settings page, remembers the token
//	pagecrawl-relay -check      self-check, says in plain words what is wrong
//	pagecrawl-relay -headless   server: reads PAGECRAWL_RELAY_TOKEN, logs, no UI
package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

// Websocket close codes the gateway uses to turn a relay away for good. Mirrored
// from scripts/relay-gateway.js; they are in the private 4000-4999 range, so no
// library constant covers them.
const (
	closeMissingToken = 4001
	closeUnauthorized = 4003
)

// Version is stamped at build time:
//
//	go build -ldflags "-X main.Version=1.2.3"
var Version = "dev"

const defaultGateway = "wss://relay.pagecrawl.io/tunnel"

type Config struct {
	GatewayURL  string
	Token       string
	Verbose     bool
	DialTimeout time.Duration
	IdleTimeout time.Duration
	MaxBackoff  time.Duration
}

func platformName() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}

func randomKey() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		// Cannot happen in practice, and the settings page is Origin-checked as well,
		// so a predictable key alone does not open it up.
		return "fallback-key"
	}

	return hex.EncodeToString(buf)
}

func main() {
	var (
		gateway  = flag.String("gateway", "", "Relay gateway websocket URL")
		token    = flag.String("token", "", "Relay token from Settings -> Relays")
		verbose  = flag.Bool("verbose", false, "Log every destination, including ones that simply did not resolve")
		headless = flag.Bool("headless", false, "Never open a settings page: for servers, containers and services")
		check    = flag.Bool("check", false, "Run a self-check and exit")
		version  = flag.Bool("version", false, "Print the version and exit")
	)
	flag.Parse()

	if *version {
		fmt.Printf("pagecrawl-relay %s (%s)\n", Version, platformName())
		return
	}

	cfg, hasToken := resolveConfig(*gateway, *token, *verbose)

	if *check {
		os.Exit(printDoctor(runDoctor(cfg, hasToken)))
	}

	state := NewState()
	state.SetPaused(loadStored().Paused)

	// A server with no token has nothing to do and should say so loudly rather than
	// sit there looking healthy.
	if !hasToken && *headless {
		log.Fatal("No token. Enrol this machine at Settings -> Relays, then set PAGECRAWL_RELAY_TOKEN.")
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	// The connection loop watches this, so pasting a token into the settings page
	// connects immediately rather than waiting out a backoff.
	restart := make(chan struct{}, 1)

	settingsURL := ""

	if !*headless {
		url, err := startUI(state, &cfg, func(string) { nudge(restart) }, func(bool) { nudge(restart) })
		if err != nil {
			log.Printf("Could not open the settings page (%v). Carrying on without it.", err)
		} else {
			settingsURL = url
			fmt.Printf("PageCrawl Relay is running.\nSettings: %s\n", url)

			// Open the browser only when there is nothing to relay yet. Someone
			// already set up does not want a window every time they log in.
			if !hasToken && !hasTray() {
				openBrowser(url)
			}
		}
	}

	go supervise(&cfg, state, restart)

	// The menu bar must own the main thread on macOS, so it blocks here instead of
	// the signal wait. Quitting from the menu returns.
	if hasTray() && !*headless {
		go func() {
			<-stop
			log.Println("Relay stopped. Your monitors fall back to PageCrawl's own proxies.")
			os.Exit(0)
		}()

		runTray(state, settingsURL, func(bool) { nudge(restart) })

		return
	}

	<-stop
	log.Println("Relay stopped. Your monitors fall back to PageCrawl's own proxies.")
}

func nudge(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

// supervise keeps the tunnel up, reconnecting with backoff.
//
// A relay lives on a laptop or a home server, so disconnects are normal rather than
// exceptional: sleep, a dropped VPN, a router reboot. Reconnecting quietly is the
// whole job.
func supervise(cfg *Config, state *State, restart chan struct{}) {
	backoff := time.Second

	for {
		if cfg.Token == "" || state.Paused() {
			// Nothing to do yet. Wait to be told the token or the pause changed,
			// rather than spinning.
			select {
			case <-restart:
			case <-time.After(5 * time.Second):
			}

			continue
		}

		conn, err := dial(*cfg)
		if err != nil {
			state.MarkDisconnected(err)
			log.Printf("Not connected (%v). Retrying in %s.", err, backoff.Round(time.Second))

			select {
			case <-restart:
				backoff = time.Second

				continue
			case <-time.After(backoff):
			}

			backoff = growBackoff(backoff, cfg.MaxBackoff)

			continue
		}

		conn.state = state
		state.MarkConnected("")
		log.Printf("Connected to %s as %s. Carrying your own monitors only.", cfg.GatewayURL, platformName())

		startedAt := time.Now()
		err = conn.Run()
		state.MarkDisconnected(err)
		log.Printf("Disconnected (%v). Carried %s this session.", err, humanBytes(conn.Bytes()))

		// A session that stayed up is evidence the setup is sound, so the next
		// hiccup starts from a short wait again. One that dropped straight away is
		// not: without this the loop redials with no pause at all, and a gateway
		// that accepts the handshake and then rejects the token spins here as fast
		// as it can answer. Measured against a bad token: two full connect and
		// reject cycles inside one second, forever.
		if time.Since(startedAt) > healthySession {
			backoff = time.Second
		}

		if isRejected(err) {
			// Retrying sooner cannot help: the token is the problem, and only the
			// person running this can fix it. Wait the longest interval and say so
			// in words rather than repeating a close code.
			backoff = cfg.MaxBackoff
			log.Printf("The gateway rejected this token. Check it in Settings -> Relays, " +
				"or remove the machine there and add it again.")
		}

		select {
		case <-restart:
			backoff = time.Second

			continue
		case <-time.After(backoff):
		}

		backoff = growBackoff(backoff, cfg.MaxBackoff)
	}
}

// How long a connection has to last before it counts as healthy. Shorter than any
// real working session and longer than a handshake-then-reject, which is the whole
// distinction being drawn.
const healthySession = 60 * time.Second

// grow the wait exponentially, with jitter so a gateway restart does not bring
// every relay in the fleet back in the same instant.
func growBackoff(backoff, max time.Duration) time.Duration {
	backoff = time.Duration(float64(backoff) * 1.7)
	if backoff > max {
		backoff = max
	}

	return backoff + jitter()
}

// isRejected reports whether the gateway turned this relay away for a reason that
// will still be true in a second: a missing, unknown or revoked token. Anything
// else (a dropped link, a gateway restart) is worth retrying promptly.
func isRejected(err error) bool {
	var closeErr *websocket.CloseError
	if errors.As(err, &closeErr) {
		return closeErr.Code == closeMissingToken || closeErr.Code == closeUnauthorized
	}

	return false
}

func jitter() time.Duration {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(time.Second)))
	if err != nil {
		return 0
	}

	return time.Duration(n.Int64())
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}

	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}
