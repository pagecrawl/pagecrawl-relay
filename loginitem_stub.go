//go:build !darwin || !tray

package main

import "errors"

// Everything except the macOS menu-bar app. Opening at login is a per-platform
// registration with the operating system, and the plain binary deliberately has no
// GUI toolkit and no CGO, so it cannot make one.
//
// Nothing is lost: a machine that should relay whenever it is on is better served by
// the service wrappers, which start before anyone logs in. See the README:
// `brew services start pagecrawl-relay`, the systemd unit, or the Docker image.

func loginItemStatus() (loginItem, error) {
	return loginItem{}, nil
}

func setLoginItem(bool) error {
	return errors.New("this build cannot open itself at login")
}
