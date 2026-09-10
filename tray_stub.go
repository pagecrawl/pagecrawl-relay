//go:build !tray

package main

// No menu bar in the default build. Kept CGO-free so one machine can cross-compile
// the binary for every platform; the menu-bar build is a separate, Mac-built
// artefact (see tray.go).
func hasTray() bool { return false }

func runTray(_ *State, _ string, _ func(bool)) {}
