//go:build !tray

package main

import (
	"context"

	"github.com/pagecrawl/pagecrawl-relay/relay"
)

// No menu bar in the default build. Kept CGO-free so one machine can cross-compile
// the binary for every platform; the menu-bar build is a separate, Mac-built
// artefact (see tray.go).
func hasTray() bool { return false }

func runTray(context.Context, *relay.Client, string) {}
