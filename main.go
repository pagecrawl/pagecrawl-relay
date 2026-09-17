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
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/pagecrawl/pagecrawl-relay/relay"
)

// Version is stamped at build time:
//
//	go build -ldflags "-X main.Version=1.2.3"
var Version = "dev"

func platformName() string {
	return runtime.GOOS + "/" + runtime.GOARCH
}

func randomKey() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	return hex.EncodeToString(buf), nil
}

func main() {
	var (
		gateway  = flag.String("gateway", "", "Relay gateway websocket URL")
		token    = flag.String("token", "", "Relay token from Settings -> Relays")
		verbose  = flag.Bool("verbose", false, "Log every destination, including ones that simply did not resolve")
		headless = flag.Bool("headless", false, "Never open a settings page: for servers, containers and services")
		check    = flag.Bool("check", false, "Run a self-check and exit")
		version  = flag.Bool("version", false, "Print the version and exit")
		open     = flag.Bool("open", false, "Open the settings page of the relay already running on this computer")
	)
	flag.Parse()

	if *version {
		fmt.Printf("pagecrawl-relay %s (%s)\n", Version, platformName())
		return
	}

	// Reopen the settings page of an instance already running here. The settings page
	// is protected by a key minted per run, so a bare visit to the port is refused;
	// this is the supported way back in without restarting the relay.
	if *open {
		url := storedUIURL()
		if url == "" {
			fmt.Println("No relay is running on this computer, or it was started with -headless.")
			fmt.Println("Start it with: pagecrawl-relay")
			os.Exit(1)
		}

		fmt.Printf("Settings: %s\n", url)
		openBrowser(url)

		return
	}

	cfg, hasToken := resolveConfig(*gateway, *token, *verbose)

	if *check {
		os.Exit(printDoctor(relay.RunDoctor(cfg)))
	}

	state := relay.NewState()
	state.SetPaused(loadStored().Paused)

	// A server with no token has nothing to do and should say so loudly rather than
	// sit there looking healthy.
	if !hasToken && *headless {
		log.Fatal("No token. Enrol this machine at Settings -> Relays, then set PAGECRAWL_RELAY_TOKEN.")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	client := relay.NewClient(cfg, state, fileStore{})
	defer client.Stop()

	settingsURL := ""

	if !*headless {
		url, err := startUI(ctx, client)
		if err != nil {
			log.Printf("Could not open the settings page (%v). Carrying on without it.", err)
		} else {
			settingsURL = url
			// So the page can be reopened later. The key is minted per run and lives
			// only in memory, so without this, closing that tab locks the operator
			// out of their own relay until they restart it.
			rememberUIURL(url)
			defer forgetUIURL()

			fmt.Printf("PageCrawl Relay is running.\nSettings: %s\n", url)

			// Open the browser only when there is nothing to relay yet. Someone
			// already set up does not want a window every time they log in.
			if !hasToken && !hasTray() {
				openBrowser(url)
			}
		}
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		relay.Supervise(ctx, client)
	}()

	if hasTray() && !*headless {
		runTray(ctx, client, settingsURL)
		cancel()
	} else {
		<-ctx.Done()
	}
	client.Stop()
	<-done
	log.Println("Relay stopped. Monitors follow their configured relay fallback setting.")
}

// printDoctor renders the checks for a terminal and returns a process exit code.
func printDoctor(results []relay.CheckResult) int {
	failed := 0

	fmt.Println()
	for _, r := range results {
		mark := "OK  "
		if !r.OK {
			mark = "FAIL"
			failed++
		}
		fmt.Printf("  [%s] %s\n         %s\n", mark, r.Name, r.Detail)
		if !r.OK && r.Fix != "" {
			fmt.Printf("         -> %s\n", r.Fix)
		}
	}
	fmt.Println()

	if failed == 0 {
		fmt.Println("  Everything checks out. This machine can relay.")
		return 0
	}

	fmt.Printf("  Checks needing attention: %d.\n", failed)

	return 1
}
