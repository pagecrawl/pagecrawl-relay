package main

import _ "embed"

// Self-contained so setup works without loading external assets.
//
//go:embed settings.html
var settingsPage string
