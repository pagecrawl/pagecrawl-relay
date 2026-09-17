# Changelog

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
