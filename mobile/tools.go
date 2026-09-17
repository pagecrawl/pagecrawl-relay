//go:build tools

// Keeps golang.org/x/mobile in go.mod. gomobile bind generates code that imports its bind
// package, but nothing in this module's own source does, so without this `go mod tidy`
// would drop the requirement and the Android build would stop working.
package mobile

import _ "golang.org/x/mobile/bind"
