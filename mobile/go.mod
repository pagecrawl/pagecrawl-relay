// A module of its own, so the Android binding's toolchain dependency (golang.org/x/mobile)
// never reaches the desktop, Docker or Home Assistant builds of the relay.
module github.com/pagecrawl/pagecrawl-relay/mobile

go 1.26.0

require (
	github.com/pagecrawl/pagecrawl-relay v0.0.0
	golang.org/x/mobile v0.0.0-20260908204917-8b95e45f8d3e
)

require (
	github.com/gorilla/websocket v1.5.3 // indirect
	golang.org/x/mod v0.41.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/tools v0.50.0 // indirect
)

replace github.com/pagecrawl/pagecrawl-relay => ../
