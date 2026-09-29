//go:build tray

// Menu-bar build. Behind a tag because the systray library needs CGO, which would
// otherwise cost the plain binary its one-machine cross-compile to every platform.
// Build it on a Mac with:
//
//	go build -tags tray -o pagecrawl-relay-menubar .
package main

import (
	"context"
	_ "embed"
	"log"
	"time"

	"fyne.io/systray"

	"github.com/pagecrawl/pagecrawl-relay/relay"
)

func hasTray() bool { return true }

// runTray owns the main thread for the life of the app. systray requires that on
// macOS, so everything else (the tunnel, the settings page) runs in goroutines
// started before this.
func runTray(ctx context.Context, client *relay.Client, settingsURL string) {
	state := client.State()
	systray.Run(func() {
		m := &menu{}
		m.setIcon(iconStarting)

		m.status = systray.AddMenuItem("Starting...", "")
		m.status.Disable()

		m.uptime = systray.AddMenuItem("", "How long this machine has been relaying")
		m.uptime.Disable()

		m.traffic = systray.AddMenuItem("", "Data carried for your monitors")
		m.traffic.Disable()

		m.exit = systray.AddMenuItem("", "The address monitored sites see")
		m.exit.Disable()

		systray.AddSeparator()

		settings := systray.AddMenuItem("Settings", "Open the settings page")
		m.pause = systray.AddMenuItem("Pause relaying", "Stop carrying checks until resumed")

		systray.AddSeparator()
		quit := systray.AddMenuItem("Quit PageCrawl Relay", "Stop relaying and exit")

		go func() {
			tick := time.NewTicker(2 * time.Second)
			defer tick.Stop()

			for {
				select {
				case <-tick.C:
					m.render(state)

				case <-settings.ClickedCh:
					openBrowser(settingsURL)

				case <-m.pause.ClickedCh:
					if _, err := client.TogglePause(); err != nil {
						log.Printf("Could not save pause: %v", err)
					}

					m.render(state)

				case <-ctx.Done():
					systray.Quit()
					return

				case <-quit.ClickedCh:
					systray.Quit()

					return
				}
			}
		}()
	}, func() {})
}

// The icon carries the state, so the menu bar stays one glyph wide. The text that
// used to sit beside it (the data carried) is a number nobody needs at all times,
// and it shoves every icon to its left along as it grows. The menu still has it.
type menu struct {
	status, uptime, traffic, exit, pause *systray.MenuItem

	// The icon on screen, so it is replaced only when the state actually changes
	// rather than on every tick.
	shown trayState
	set   bool
}

func (m *menu) setIcon(s trayState) {
	if m.set && m.shown == s {
		return
	}

	icon := trayIconOffline

	switch s {
	case iconRelaying:
		icon = trayIconRelaying
	case iconPaused:
		icon = trayIconPaused
	}

	systray.SetTemplateIcon(icon, icon)
	systray.SetTooltip(s.tooltip())

	m.shown, m.set = s, true
}

// render keeps the menu readable at a glance: the icon answers "is it working" from
// the menu bar, and the first line spells the same thing out in words.
func (m *menu) render(state *relay.State) {
	lines := linesFor(state.Snapshot())

	m.setIcon(lines.state)
	m.status.SetTitle(lines.status)
	m.uptime.SetTitle(lines.uptime)
	m.traffic.SetTitle(lines.traffic)
	m.exit.SetTitle(lines.exit)
	m.pause.SetTitle(lines.pause)
}

// 32x32 template icons, one per state, drawn by packaging/macos/trayicon. A template
// is recoloured by macOS, so one file each covers light and dark menu bars, and 32
// pixels drawn at 16 points keeps them sharp on a Retina display.
//
//go:embed icon.png
var trayIconRelaying []byte

//go:embed icon-paused.png
var trayIconPaused []byte

//go:embed icon-offline.png
var trayIconOffline []byte
