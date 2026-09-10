//go:build tray

// Menu-bar build. Behind a tag because the systray library needs CGO, which would
// otherwise cost the plain binary its one-machine cross-compile to every platform.
// Build it on a Mac with:
//
//	go build -tags tray -o pagecrawl-relay-menubar .
package main

import (
	_ "embed"
	"fmt"
	"time"

	"fyne.io/systray"
)

func hasTray() bool { return true }

// runTray owns the main thread for the life of the app. systray requires that on
// macOS, so everything else (the tunnel, the settings page) runs in goroutines
// started before this.
func runTray(state *State, settingsURL string, onPause func(bool)) {
	systray.Run(func() {
		systray.SetTemplateIcon(trayIcon, trayIcon)
		systray.SetTooltip("PageCrawl Relay")

		status := systray.AddMenuItem("Starting...", "")
		status.Disable()

		uptime := systray.AddMenuItem("", "How long this machine has been relaying")
		uptime.Disable()

		traffic := systray.AddMenuItem("", "Data carried for your monitors")
		traffic.Disable()

		exit := systray.AddMenuItem("", "The address monitored sites see")
		exit.Disable()

		systray.AddSeparator()

		settings := systray.AddMenuItem("Settings and self-check", "Open the settings page")
		pause := systray.AddMenuItem("Pause relaying", "Stop carrying checks until resumed")

		systray.AddSeparator()
		quit := systray.AddMenuItem("Quit PageCrawl Relay", "Stop relaying and exit")

		go func() {
			tick := time.NewTicker(2 * time.Second)
			defer tick.Stop()

			for {
				select {
				case <-tick.C:
					render(state, status, uptime, traffic, exit, pause)

				case <-settings.ClickedCh:
					openBrowser(settingsURL)

				case <-pause.ClickedCh:
					paused := !state.Paused()
					state.SetPaused(paused)

					saved := loadStored()
					saved.Paused = paused
					_ = saveStored(saved)

					if onPause != nil {
						onPause(paused)
					}

					render(state, status, uptime, traffic, exit, pause)

				case <-quit.ClickedCh:
					systray.Quit()

					return
				}
			}
		}()
	}, func() {})
}

// render keeps the menu readable at a glance: the title line carries the state, so
// the answer to "is it working" is visible without opening anything.
func render(state *State, status, uptime, traffic, exit, pause *systray.MenuItem) {
	s := state.Snapshot()

	switch {
	case s.Paused:
		status.SetTitle("Paused")
		systray.SetTitle("")
	case s.Connected:
		status.SetTitle("Connected")
		// Data carried sits in the menu bar itself: it is the number people want to
		// keep an eye on, because it is their bandwidth.
		systray.SetTitle(s.Traffic)
	default:
		status.SetTitle("Not connected")
		systray.SetTitle("!")
	}

	if s.Connected && s.ConnectedFor != "" {
		uptime.SetTitle("Connected for " + s.ConnectedFor)
	} else {
		uptime.SetTitle("Running for " + s.Uptime)
	}

	traffic.SetTitle(fmt.Sprintf("%s carried, %d connections", s.Traffic, s.Connections))

	if s.ExitIP != "" {
		exit.SetTitle("Your address: " + s.ExitIP)
	} else {
		exit.SetTitle("Your address: run a check")
	}

	if s.Paused {
		pause.SetTitle("Resume relaying")
	} else {
		pause.SetTitle("Pause relaying")
	}
}

// A 16x16 template icon. macOS recolours a template automatically for light and
// dark menu bars, so one asset covers both.
//
//go:embed icon.png
var trayIcon []byte
