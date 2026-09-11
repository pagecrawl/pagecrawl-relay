# PageCrawl Relay client

A small program you run on a computer you own. Your PageCrawl checks then leave
from **your** connection instead of ours, which is what you want for pages only
your public IP can reach: a public portal that allows your office IP, or a
site that only serves your country. Private intranets and LAN addresses are refused.

It is not a general-purpose proxy. The gateway authorizes checks for your team,
and the client independently validates each destination. HTTPS page content stays
encrypted between PageCrawl and the site. Plain HTTP content is unencrypted and
visible to the relay machine; hostnames and ports are visible for both.

**Available on every plan, including Free.** Some sites answer a datacenter address
with a 403, a CAPTCHA, or a stripped-down page, while serving a normal one to an
ordinary home or office connection. If a page works in your browser but not in
PageCrawl, this is the thing to try. It costs nothing beyond your own bandwidth.

Open source and small enough to read: see [Why this is open source](#why-this-is-open-source).
Security reporting is in [SECURITY.md](SECURITY.md).

---

## Pick your setup

| You have | Go to |
|---|---|
| A Mac or Linux machine, and you are happy with one command | [Homebrew](#install-with-homebrew) |
| A Mac or Windows PC, and you have never used a terminal | [1. Just run it](#1-just-run-it) |
| A Linux box or home server you want it running on permanently | [2. Run it as a service](#2-run-it-as-a-service) |
| Docker, a NAS, or a homelab | [3. Docker](#3-docker) |
| Home Assistant | [the add-on](https://github.com/pagecrawl/hass-relay-addon) |
| A question about whether it is actually working | [4. Check it](#4-check-it) |

First, in PageCrawl: **Settings → Relays → Add machine**. Copy the token it shows.
It is shown once.

---

## Install with Homebrew

The shortest route on macOS and Linux, and the one that keeps working:

```bash
brew install pagecrawl/tap/pagecrawl-relay
pagecrawl-relay
```

That opens a settings page in your browser; paste the token and press **Connect**.

To update later:

```bash
brew upgrade pagecrawl-relay
```

For a machine that should relay whenever it is awake:

```bash
brew services start pagecrawl-relay
```

**Note:** the binaries are not code-signed yet, so macOS may warn when opening a
browser download. Homebrew provides a convenient install and update path. See
[macOS says it cannot check the app for malware](#macos-says-it-cannot-check-the-app-for-malware)
for other installation options and download verification.

---

## 1. Just run it

No terminal needed.

1. Download the app for your computer and open it.
2. A settings page opens in your browser.
3. Paste the token and press **Connect**.

Closing the page leaves the relay running; quitting the app stops it.

### Settings page

The local settings page uses PageCrawl's logo and brand styles, adapts to small
screens, and follows your system's light or dark appearance. Its logo, styles and
scripts are bundled with the client, so the page loads without internet access.
Relaying and network diagnostics still need a connection.

| Control or section | What it does |
|---|---|
| Connection status | Shows connection time, uptime, data carried and the most recent destination connected to. Counters reset when the app restarts. |
| Pause / Resume | Stops active connections without forgetting the token. The paused state survives a restart. |
| Run a check | Checks connectivity and authentication without interrupting the running tunnel. |
| Recent activity | Shows the last 40 successful destinations and policy refusals. This list stays in memory and resets on restart. |
| Disconnect this machine | Closes connections and forgets the saved token. Remove the machine in PageCrawl separately to delete its listing. |

**Reopening the settings page.** The page keeps its access key in the current tab,
so reloading works. To open a new tab or return after restarting the relay:

```bash
pagecrawl-relay -open
```

That opens the page for the relay already running under your user account. The app
opens it automatically on first setup; later starts run without opening a browser.
The page listens only on this computer. Opening its bare address in a fresh tab
does not grant access to its status or controls.

**About the menu bar.** The published binaries have no menu-bar icon. It needs CGO and
a Mac to build on, which would cost the plain binary its one-machine cross-compile to
every platform, so it is a separate build (`go build -tags tray`). The settings page is
the interface for the published builds.

**Where your token is kept:** `config.json` in your user config folder:
`~/Library/Application Support/pagecrawl-relay/` on macOS,
`~/.config/pagecrawl-relay/` on Linux (or `$XDG_CONFIG_HOME/pagecrawl-relay/`), or
`%AppData%\pagecrawl-relay\` on Windows. On Unix the file is written with mode
`0600`; Windows uses the access permissions of your user profile.

**A machine that sleeps stops relaying.** With fallback enabled, checks can use
PageCrawl's proxies while it is away. Retry and error policies keep content checks
on the selected relay and defer them until it returns; the error policy also
reports the offline failure. For reliable availability, use a machine that stays awake.

### macOS says it cannot check the app for malware

The binaries are not signed with an Apple Developer ID, so macOS may block a
download it cannot verify. [Verify the download](#checking-that-a-download-is-the-real-thing)
before choosing to run it.

You can install through Homebrew as shown above, or download from the public
release with `curl`:

```bash
curl -fsSL -o pagecrawl-relay \
  https://github.com/pagecrawl/pagecrawl-relay/releases/latest/download/pagecrawl-relay-darwin-arm64
chmod +x pagecrawl-relay
./pagecrawl-relay
```

Use `pagecrawl-relay-darwin-amd64` on an Intel Mac.

If you already downloaded and verified it, clear the quarantine flag on that file:

```bash
xattr -d com.apple.quarantine ~/Downloads/pagecrawl-relay-darwin-arm64
chmod +x ~/Downloads/pagecrawl-relay-darwin-arm64
```

Or build it yourself with Go:

```bash
go install github.com/pagecrawl/pagecrawl-relay@latest
```

### Checking that a download is the real thing

Every release is built by GitHub Actions from the tagged commit, and each binary
carries a signed provenance attestation. You can verify that the file you have was
built by that workflow from this source, rather than uploaded by hand:

```bash
gh attestation verify pagecrawl-relay-darwin-arm64 --repo pagecrawl/pagecrawl-relay
```

Every release also ships `SHA256SUMS`:

```bash
shasum -a 256 -c SHA256SUMS --ignore-missing
```

The attestation identifies the source and build workflow. It serves a different
purpose from platform code signing and does not replace reviewing the source.

Windows SmartScreen may also warn ("Windows protected your PC"). After verifying
the download, choose **More info** then **Run anyway**, or build from source.

---

## 2. Run it as a service

For a Linux box, a home server, or a Mac mini in a cupboard.

If you installed with Homebrew, this is already done for you and needs no unit file:

```bash
brew services start pagecrawl-relay
```

Otherwise, with systemd:

```bash
# Download the binary for your platform, then:
sudo cp pagecrawl-relay /usr/local/bin/
sudo cp pagecrawl-relay.service /etc/systemd/system/

# Put the token in an override so it stays out of the unit file:
sudo systemctl edit pagecrawl-relay
#   [Service]
#   Environment=PAGECRAWL_RELAY_TOKEN=your-token-here

sudo systemctl enable --now pagecrawl-relay
```

Check the service, then watch its logs:

```bash
sudo systemctl status pagecrawl-relay
journalctl -u pagecrawl-relay -f
```

For a self-check, set `PAGECRAWL_RELAY_TOKEN` in your shell and run
`pagecrawl-relay -check`. Your shell does not inherit the token from the systemd
override. This diagnostic does not interrupt the running service.

The bundled unit uses an unprivileged `DynamicUser`, restricted filesystem access
and a private temporary directory. It only needs to make outbound connections.

**Not systemd?** The binary is one file with no dependencies. Anything that keeps a
process alive works: launchd, runit, supervisor, even a `screen` session. It needs
one environment variable, `PAGECRAWL_RELAY_TOKEN`, and the `-headless` flag.

---

## 3. Docker

From this directory, Compose builds the bundled Dockerfile:

```bash
echo "PAGECRAWL_RELAY_TOKEN=your-token-here" > .env
docker compose up -d
```

Or build and run the image without Compose:

```bash
docker build -t pagecrawl-relay .
docker run -d --name pagecrawl-relay --restart unless-stopped \
  -e PAGECRAWL_RELAY_TOKEN=your-token-here \
  pagecrawl-relay
```

**No ports are published, and none should be.** The client dials out; nothing ever
connects to it. If a guide tells you to open a port, it is not this one.

The image has a healthcheck that runs the real self-check, so a revoked token turns
the container unhealthy instead of quietly relaying nothing:

```bash
docker compose exec relay pagecrawl-relay -check
docker inspect --format '{{.State.Health.Status}}' pagecrawl-relay
```

**On a NAS** (Synology, unRAID, TrueNAS): build the image on the NAS or use its
Compose support, set `PAGECRAWL_RELAY_TOKEN`, enable automatic restart and publish
no ports.

**On Home Assistant** there is a proper add-on, so none of the above is needed:
add the repository, paste the token into the Configuration tab, start it. See
[the Home Assistant add-on](https://github.com/pagecrawl/hass-relay-addon). Home Assistant boxes are
usually on all the time and sitting on a home connection, which makes them the best
relay host most people already own.

---

## 4. Check it

One command answers "is this working", in plain language:

```bash
pagecrawl-relay -check
```

It reports, in order: whether a token is configured, whether the gateway name
resolves, whether this machine can make outbound connections, whether the gateway
**accepts this machine**, whether the local-network protection is active, and the
public address monitored sites will see. Authentication uses a diagnostic
connection that does not replace the running tunnel or interrupt its checks.

Anything that fails says what to do about it. The desktop app runs the same checks
behind **Run a check** on the settings page.

To confirm end to end, in PageCrawl set a monitor's Location to your machine (or
**Any of my machines**) and run a check. Watching a page like
`whatismyipaddress.com` is the clearest proof: the captured content will show your
address instead of ours.

### When it is not relaying

With the default fallback policy, an unavailable relay can cause a check to use
PageCrawl's proxies. Retry and error policies defer content checks until a relay is
available; the error policy also marks the check as failed. Review that setting
when the source IP must stay on your relay:

| Symptom | Cause |
|---|---|
| Machine shows Offline | Not running, asleep, or no outbound connection. `-check` will say which. |
| Connected, but checks do not use it | The monitor's Location is not set to your machine. |
| Worked, then stopped | Monthly data limit reached, or the machine was paused in Settings. |
| Some subresources missing | Your DNS filters ads or trackers, so those hosts do not resolve here. Harmless, but pages can render slightly differently than from our proxies. |

---

## What it costs the machine it runs on

Almost nothing in CPU and memory, and rather more in bandwidth. It moves bytes
between the site and PageCrawl; it does not open a browser, run JavaScript or parse
pages, which is the expensive part and stays on PageCrawl's side.

| Resource | What to expect |
|---|---|
| Memory | Depends on traffic and runtime overhead. The client allows 64 active or closing streams, caps destination queues at 4 MiB per stream and 8 MiB in aggregate, and caps the gateway write queue at 8 MiB. |
| CPU | Idle between checks, and a fraction of one core while carrying them. Measured at 0% idle. |
| Disk | About 10 MB for the program. It stores no page content and keeps no database. |
| Ports | One persistent outbound gateway tunnel, plus outbound connections to destination sites. Desktop mode also listens on loopback for the settings page. |

That is comfortable on a Raspberry Pi, a NAS, a Home Assistant box or an old laptop.
The hardware is not the constraint; the connection is.

### Bandwidth depends on how many pages you check

Every relayed page is downloaded from the site by this machine and then sent on to
PageCrawl, so it uses **both** your download and your upload allowance, roughly the
page's weight in each direction.

A rough guide, taking a standard page at 2 MB (a plain text page is nearer 0.5 MB,
an image-heavy retail page 5 MB or more):

| Monitors on this machine | Every hour | Every 6 hours | Once a day |
|---|---|---|---|
| 1 | ~3 GB/month | ~500 MB/month | ~120 MB/month |
| 5 | ~14 GB/month | ~2.4 GB/month | ~600 MB/month |
| 20 | ~58 GB/month | ~10 GB/month | ~2.4 GB/month |

The arithmetic is `monitors x checks per month x page size x 2`. The settings page's
**Data carried** counter counts forwarded bytes once and resets when the app
restarts. Your internet connection carries both network legs, so its total usage
is roughly twice that counter, plus protocol overhead.

### What the same traffic would cost as paid bandwidth

This is the number that makes a relay worth running. PageCrawl also sells metered
residential bandwidth at $10 per gigabyte on the Ultimate and Enterprise plans, and
the same pages carried that way are billed by the gigabyte:

| Monitors, checked hourly | Through this machine | As paid residential bandwidth |
|---|---|---|
| 1 | your own connection | about $14/month |
| 5 | your own connection | about $72/month |
| 20 | your own connection | about $288/month |

To price your own pages rather than this example, use the calculator at
[pagecrawl.io/residential-proxies](https://pagecrawl.io/residential-proxies#cost-calculator);
it takes your page size, check frequency and number of monitors.

Metered bandwidth still wins when you need a page checked from a country you are not
in, or you would rather your own address stayed out of it. A relay wins when the page
simply needs to see an ordinary connection and you already have a machine that is on.

Watch the upload side in particular. Home connections often have a tenth of the
download speed on upload, and a page that takes seconds to fetch can take longer to
hand back.

Set a monthly limit per machine under **Settings → Relays**, and relay only the
monitors that need it. When a machine reaches its limit, checks follow the chosen
offline policy: fallback permits PageCrawl's proxies, while retry and error keep
content checks on the relay. A page that works from PageCrawl's own addresses may
not need relaying.

---

## Options

| Flag | Environment variable | Meaning |
|---|---|---|
| `-token` | `PAGECRAWL_RELAY_TOKEN` | the token from Settings → Relays |
| `-gateway` | `PAGECRAWL_RELAY_GATEWAY` | gateway URL, only for a self-hosted PageCrawl |
| `-headless` | | never open a settings page: services and containers |
| `-open` | | reopen the settings page of a running desktop relay |
| `-check` | | run the self-check and exit |
| `-verbose` | | also log ordinary destination resolution failures |
| `-version` | | print the version |

Precedence is flag, then environment variable, then the saved config file, so a
service's environment is never overridden by a stale desktop config.

---

## Build

Run these commands from the root of this repository. Releases use Go 1.27.1.
The settings page and its branding
are embedded from `settings.html`; rebuild the client after editing that file.
No separate frontend build is needed.

```bash
go build -ldflags "-X main.Version=$(git describe --tags --always)" -o pagecrawl-relay .
```

Pure Go, no CGO, so it cross-compiles with `GOOS`/`GOARCH` alone:

```bash
CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -o dist/pagecrawl-relay-linux-amd64 .
CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -o dist/pagecrawl-relay-darwin-arm64 .
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o dist/pagecrawl-relay-windows-amd64.exe .
```

The macOS menu-bar build needs CGO and a Mac to build on:

```bash
go build -tags tray -o pagecrawl-relay-menubar .
```

### Tests

From the directory containing this README:

```bash
go test -race ./...
go vet ./...
```

Tests cover destination guards, frame decoding, diagnostic authentication, config
precedence and permissions, settings authorization, pause and token changes,
socket cleanup, and bounded queues. They use local test servers without a live
PageCrawl account.

On Linux, also exercise 32-bit decoding:

```bash
CGO_ENABLED=0 GOARCH=386 go test ./...
```

For the optional browser regression, install Node.js with npm, then install the
test dependency and its Chromium browser in this checkout:

```bash
npm install --no-save --package-lock=false playwright
npx playwright install chromium
node --test settings.browser.test.cjs
```

This builds and starts the real client, then checks setup, reload authorization,
pause, disconnect, diagnostic escaping and error recovery under the page's
Content Security Policy.

The release workflow runs race tests, vet, 32-bit tests and a dependency
vulnerability scan. Run the scan locally with:

```bash
go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...
```

---

## Why this is open source

You are being asked to run a program on your own network that makes outbound
connections on someone else's behalf. That is a lot to take on trust, so the code
is MIT licensed. The default build uses Go's standard library and a websocket
library; the optional tray build adds a GUI dependency.

**If you only read one file, read `guard.go`.** It is what decides whether a
destination may be reached, and it is the difference between a relay and a hole in
your firewall.

Four ways to check the claims below rather than believing them:

```bash
# 1. Open the running desktop relay's status and recent destinations.
pagecrawl-relay -open

# 2. What it listens on. Only a loopback settings port; nothing on your network.
lsof -nP -iTCP -sTCP:LISTEN -a -p "$(pgrep -f pagecrawl-relay)"

# 3. That the guard is on, and that your own network is refused.
pagecrawl-relay -check

# 4. Verify the downloaded binary's source and build workflow.
gh attestation verify pagecrawl-relay-darwin-arm64 --repo pagecrawl/pagecrawl-relay
```

A checksum comparison requires the same source commit, Go toolchain, target and
build flags as the release. Use the release workflow and provenance attestation
to establish those inputs; a different local build can have a different checksum.

### What is open, what is not, and why

**Open, because you run it and deserve to read it:**

- this client, in full: the tunnel, the destination guard, the settings page, the
  self-check, and every test;
- the wire format it speaks, which is documented in `protocol.go`;
- the Docker, systemd and Home Assistant packaging.

The hosted gateway and PageCrawl service are separate from this public client.

The client independently enforces its destination and resource limits. It still
trusts the gateway to request the intended public sites and enforce team access.
The activity list shows requested destinations, including policy refusals.

## How traffic and destination checks work

1. The client opens an outbound authenticated websocket to the gateway. Desktop
   mode also serves a settings page on loopback. No router port forwarding is needed.
2. The gateway asks it to connect to a destination for a monitor. The client
   resolves the name, filters refused addresses, and dials an approved IP directly.
3. Bytes flow between the gateway and destination. HTTPS encrypts page content
   between PageCrawl and the site; plain HTTP does not. The relay sees the
   destination hostname and port in either case.
4. Finishing, pausing or disconnecting closes sockets and cancels pending work.

The guard blocks private and special address ranges, including common cloud
metadata addresses and IPv6 translation prefixes. It does not infer what service
a public address runs. Network-specific routing of public addresses remains an
operator consideration. See [SECURITY.md](SECURITY.md) for exact boundaries.

## Can PageCrawl tell whether I modified this?

No, and it is built so that it does not need to.

Anyone can patch software they run on their own computer. Any claim that we can
detect that would be false, and worse, it would mean other customers' safety
depended on a check that cannot hold. So we assume every client is modified and
enforce the rules that matter on our side instead: which team a machine may carry
work for, how much data it may use, and what it may be asked to reach.

Removing the local guard weakens protection for that machine. Gateway checks
remain responsible for cross-team authorization and usage limits, regardless
of which client binary is running. [SECURITY.md](SECURITY.md) describes both sides.

## Layout

| file | role |
|---|---|
| `main.go` | flags, modes, the reconnect loop |
| `config.go` | flag/env/file precedence, saved token |
| `client.go` | synchronized settings, session cancellation |
| `tunnel.go` | websocket to the gateway, stream multiplexing |
| `protocol.go` | the wire format spoken with the gateway |
| `guard.go` | destination validation, the part that protects the operator |
| `doctor.go` | the self-check behind `-check` |
| `ui.go` | authenticated local settings API |
| `page.go`, `settings.html` | embedded settings page, bundled logo and styles |
| `state.go` | live status shared by the page and the menu bar |
| `*_test.go`, `settings.browser.test.cjs` | Go regressions and the optional Chromium test |

The gateway speaks the same frame format, and both sides have codec tests covering
partial frames and binary payloads.
