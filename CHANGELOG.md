# Changelog

## v0.1.7

- **PageCrawl Relay for Mac.** A menu-bar app, released as `pagecrawl-relay-macos.dmg`:
  one app for Apple Silicon and Intel Macs running macOS 13 or later. The menu shows
  whether it is connected, the data carried and the address sites see, and pauses,
  resumes or opens the settings page. On first launch it opens the settings page so the
  token can be pasted straight away.
- **The Mac menu bar shows the state as an icon**, and nothing else: a filled mark
  while relaying, pause bars when paused, and an empty outline when it is not
  connected. The data carried no longer sits in the menu bar, where it was a number
  that changed constantly and pushed the icons beside it along; it is still in the
  menu, with the connection count. A token the gateway has refused now says so in
  the menu instead of looking like an ordinary disconnection.
- **Open at login**, a switch on the settings page in the Mac app, so a machine
  relays from the moment you log in without being started by hand. It registers the
  app with macOS rather than a hidden helper, so it is listed under System Settings,
  General, Login Items like anything else, and turning it off there is respected.
- The settings page links to Settings then Relays in PageCrawl, where a machine is
  renamed, given a monthly data limit or removed.
- The self-check now records the address it finds, so **Your address** shows it
  instead of asking for a check that never filled it in.
- **Signed and notarized for macOS.** The app and the `pagecrawl-relay-darwin-*`
  binaries are signed with an Apple Developer ID and notarized, so macOS opens them
  without a malware warning. Asset names are unchanged.

## v0.1.6

- **PageCrawl Relay for Android.** A separate app that turns an Android phone into a
  relay, running the same destination guard and protocol as the desktop program. Add it
  by scanning the QR code under Settings, Relays, Add machine, or by pasting the token.
  It relays on Wi-Fi by default, with mobile data as an opt-in, restarts after a reboot
  if it was on, and has the same self-check as `-check`. Released as
  `pagecrawl-relay-android.apk`, signed and attested like the desktop binaries.
- The relay itself now lives in its own `relay` package, which the desktop program
  wraps. This is what lets the Android app run the same guard and protocol rather than
  a copy of them. Nothing changes for the desktop, Docker or Home Assistant builds: the
  binary, its flags, its settings file and `-X main.Version` all work as before.
- The status now reports when the gateway rejected the token, separately from an
  ordinary disconnection, so a front end can say the token needs replacing instead of
  retrying indefinitely.
- **A published Docker image.** Each release now pushes
  `ghcr.io/pagecrawl/pagecrawl-relay` for amd64 and arm64, with a signed provenance
  attestation, so a NAS or a homelab box can pull the relay instead of building it.
  Compose uses it by default; building the bundled Dockerfile still works unchanged.

## v0.1.5

- Pause, disconnect and token changes now close active connections and cancel
  pending work before reporting success.
- Self-checks verify that the gateway accepts the token without replacing the
  running tunnel or interrupting checks.
- Block additional private-network and cloud-metadata routes, including IPv6
  address translations that could bypass destination checks.
- Limit queued data and concurrent connections so slow destinations cannot stall
  the whole relay. Fix malformed-frame handling on 32-bit systems.
- Add the PageCrawl logo and brand styles to the settings page, and improve its
  mobile and dark-mode layouts. The page remains available offline.
- Repair existing token-file permissions on Unix and prevent partial token saves.
  Exclude local credentials from source control and Docker build contexts.

Existing relay tokens remain valid. Restart the client after updating.
Custom gateways must support the authenticated diagnostic response for self-checks
to succeed.
