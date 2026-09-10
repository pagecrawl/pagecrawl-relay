# PageCrawl Relay client

A small program you run on a computer you own. Your PageCrawl checks then leave
from **your** connection instead of ours, which is what you want for pages only
your network can reach: an intranet, a portal that allows your office IP, or a
site that only serves your country.

It is not a general-purpose proxy. It opens no port for anything else, it carries
only your own team's monitors, and it never sees page content (the encryption is
between PageCrawl and the site; this only moves the bytes).

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
| A Mac or Windows PC, and you have never used a terminal | [1. Just run it](#1-just-run-it) |
| A Linux box or home server you want it running on permanently | [2. Run it as a service](#2-run-it-as-a-service) |
| Docker, a NAS, or a homelab | [3. Docker](#3-docker) |
| Home Assistant | [the add-on](../hass-relay-addon/) |
| A question about whether it is actually working | [4. Check it](#4-check-it) |

First, in PageCrawl: **Settings → Relays → Add machine**. Copy the token it shows.
It is shown once.

---

## 1. Just run it

No terminal needed.

1. Download the app for your computer and open it.
2. A settings page opens in your browser.
3. Paste the token and press **Connect**.

That is the whole setup. The page shows whether it is connected, how much data it
has carried, and which site it most recently fetched. Closing the page leaves the
relay running; quitting the app stops it.

On macOS you also get a menu-bar icon showing status, uptime and data used.

**Where your token is kept:** your user config folder
(`~/Library/Application Support/pagecrawl-relay/config.json` on macOS,
`%AppData%\pagecrawl-relay\` on Windows), readable only by you.

**A machine that sleeps stops relaying.** That is fine: checks fall back to
PageCrawl's own proxies while it is away and pick your machine up again when it
returns. If you want your connection used reliably, run it on something that stays
awake, which is what the next two sections are for.

### macOS says it cannot check the app for malware

It will, until the builds are signed with an Apple Developer ID. The binaries are not
signed yet, and macOS quarantines anything downloaded through a browser from a
developer it cannot verify. Nothing is wrong with the file: the warning is about who
vouches for it, not what it contains.

Downloading with `curl` avoids it entirely, because the quarantine flag is set by the
browser rather than by macOS itself:

```bash
curl -fsSL -o pagecrawl-relay \
  https://github.com/pagecrawl/pagecrawl-relay/releases/latest/download/pagecrawl-relay-darwin-arm64
chmod +x pagecrawl-relay
./pagecrawl-relay
```

Use `pagecrawl-relay-darwin-amd64` on an Intel Mac.

If you already downloaded it in a browser, clear the flag on that file:

```bash
xattr -d com.apple.quarantine ~/Downloads/pagecrawl-relay-darwin-arm64
chmod +x ~/Downloads/pagecrawl-relay-darwin-arm64
```

Or build it yourself, which produces the same program and no warning at all:

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

This is a stronger statement than a code signature, which only says that some identity
paid for a certificate. It says which source code produced this exact file.

Windows SmartScreen shows an equivalent warning ("Windows protected your PC"), for the
same reason: choose **More info** then **Run anyway**, or build from source.

---

## 2. Run it as a service

For a Linux box, a home server, or a Mac mini in a cupboard.

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

Check it, then watch it:

```bash
pagecrawl-relay -check
journalctl -u pagecrawl-relay -f
```

The bundled unit runs under `DynamicUser` with no home directory, no privileges
and no filesystem write access. It only needs to make outbound connections.

**Not systemd?** The binary is one file with no dependencies. Anything that keeps a
process alive works: launchd, runit, supervisor, even a `screen` session. It needs
one environment variable, `PAGECRAWL_RELAY_TOKEN`, and the `-headless` flag.

---

## 3. Docker

```bash
echo "PAGECRAWL_RELAY_TOKEN=your-token-here" > .env
docker compose up -d
```

Or without compose:

```bash
docker run -d --name pagecrawl-relay --restart unless-stopped \
  -e PAGECRAWL_RELAY_TOKEN=your-token-here \
  ghcr.io/pagecrawl/relay:latest
```

**No ports are published, and none should be.** The client dials out; nothing ever
connects to it. If a guide tells you to open a port, it is not this one.

The image has a healthcheck that runs the real self-check, so a revoked token turns
the container unhealthy instead of quietly relaying nothing:

```bash
docker compose exec relay pagecrawl-relay -check
docker inspect --format '{{.State.Health.Status}}' pagecrawl-relay
```

**On a NAS** (Synology, unRAID, TrueNAS): add the image through the usual container
UI, set `PAGECRAWL_RELAY_TOKEN` as an environment variable, set restart to always,
and publish no ports. That is the entire configuration.

**On Home Assistant** there is a proper add-on, so none of the above is needed:
add the repository, paste the token into the Configuration tab, start it. See
[`packages/hass-relay-addon`](../hass-relay-addon/). Home Assistant boxes are
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
public address monitored sites will see.

Anything that fails says what to do about it. The desktop app runs the same checks
behind **Run a check** on the settings page.

To confirm end to end, in PageCrawl set a monitor's Location to your machine (or
**Any of my machines**) and run a check. Watching a page like
`whatismyipaddress.com` is the clearest proof: the captured content will show your
address instead of ours.

### When it is not relaying

Everything below falls back to PageCrawl's own proxies rather than failing the
check, so the symptom is "the check worked but used the wrong address" rather than
an error:

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
| Memory | About 15 MB idle. Each check in flight adds tens of kilobytes of buffer, so it stays well under 40 MB even when busy. |
| CPU | Idle between checks, and a fraction of one core while carrying them. Measured at 0% idle. |
| Disk | About 10 MB for the program. It stores no page content and keeps no database. |
| Ports | One outbound connection to the gateway. In desktop mode it also listens on loopback for the settings page, and on nothing else. |

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

The arithmetic is `monitors x checks per month x page size x 2`. The settings page shows
what this machine has actually carried, which beats any estimate.

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

Two things keep it in hand: set a monthly limit per machine in PageCrawl under
**Settings → Relays** (once reached, checks go back to PageCrawl's proxies on their
own rather than failing), and relay only the monitors that need it. A page that
works fine from PageCrawl's own addresses gains nothing from being relayed.

---

## Options

| Flag | Environment variable | Meaning |
|---|---|---|
| `-token` | `PAGECRAWL_RELAY_TOKEN` | the token from Settings → Relays |
| `-gateway` | `PAGECRAWL_RELAY_GATEWAY` | gateway URL, only for a self-hosted PageCrawl |
| `-headless` | | never open a settings page: services and containers |
| `-check` | | run the self-check and exit |
| `-verbose` | | log every destination, including ones that did not resolve |
| `-version` | | print the version |

Precedence is flag, then environment variable, then the saved config file, so a
service's environment is never overridden by a stale desktop config.

---

## Build

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

Tests: `go test ./...` covers the destination guard, the frame codec and the
keepalive timings.

---

## Why this is open source

You are being asked to run a program on your own network that makes outbound
connections on someone else's behalf. That is a lot to take on trust, so the code
is MIT licensed and this is the whole of it: about 900 lines, no dependencies
beyond a websocket library, readable in an afternoon.

**If you only read one file, read `guard.go`.** It is what decides whether a
destination may be reached, and it is the difference between a relay and a hole in
your firewall.

Four ways to check the claims below rather than believing them:

```bash
# 1. What it is doing right now, destination by destination.
pagecrawl-relay -verbose

# 2. What it listens on. Only a loopback settings port; nothing on your network.
lsof -nP -iTCP -sTCP:LISTEN -a -p "$(pgrep -f pagecrawl-relay)"

# 3. That the guard is on, and that your own network is refused.
pagecrawl-relay -check

# 4. That the published binary is the code you just read.
go build -trimpath -ldflags "-s -w -X main.Version=$(cat VERSION)" -o built .
shasum -a 256 built pagecrawl-relay-darwin-arm64
```

The last one is the important one: builds are reproducible (`-trimpath`, pinned Go
version, no CGO in the default binary), so a checksum you compute matches the one
published with the release. If it does not, do not run it and please tell us.

### What is open, what is not, and why

**Open, because you run it and deserve to read it:**

- this client, in full: the tunnel, the destination guard, the settings page, the
  self-check, and every test;
- the wire format it speaks, which is documented in `protocol.go`;
- the Docker, systemd and Home Assistant packaging.

**Not open, because it is ours rather than yours:**

- the gateway that terminates the tunnel on PageCrawl's side;
- how PageCrawl decides a page needs a different route, schedules checks, scores
  changes, and everything else the product does.

The line is not about secrecy for its own sake. It is this: **everything that runs
on your computer or touches your network is open.** Nothing in the closed half can
change what this program will or will not do on your machine, which is why reading
this repository is enough to judge it. The closed half holds no secret of yours, and
you can watch everything it asks your machine to do in the activity list.

## How it works, and why it cannot see into your network

**The short version.** PageCrawl's server opens the page as usual. The only thing
that changes is which door the traffic goes out of: instead of leaving from a data
centre, it leaves from your machine. Your computer passes bytes along without being
able to read them, the way a postal sorting office moves a sealed envelope.

**The longer version**, because "trust us" is not an answer:

1. Your machine makes one outgoing connection to PageCrawl and holds it open. It
   never accepts an incoming one. There is nothing to forward on your router, and
   nothing on the internet can reach it.
2. When one of **your** monitors is due, PageCrawl asks your machine to open a
   connection to that site, and only that site.
3. Your machine checks the address is a real public one, not something on your own
   network, and then connects.
4. Encrypted bytes flow back and forth. The encryption is between PageCrawl and the
   site, so your machine cannot read the page even though it carried it. Neither can
   anyone watching your network.
5. When the check finishes, the permission that allowed it stops working.

**Why it cannot browse your network.** Your machine never decides where to connect;
PageCrawl asks, and every request is checked against a refusal list before anything
happens. Your router, your NAS, your printer, anything on `192.168.x` or `10.x`, and
the addresses cloud servers use for their own configuration are all refused. The
check happens after the address is looked up and before the connection is made, so a
web address that secretly points at your router is refused too. You can watch this
happen live: refusals appear in the settings page's activity list, and
`-check` proves the guard is switched on.

**Why it cannot be used against you by us.** The permission PageCrawl issues is for
one check, on one monitor, belonging to your team. It cannot be reused for another
customer, and it stops working when the check ends.

## Can PageCrawl tell whether I modified this?

No, and it is built so that it does not need to.

Anyone can patch software they run on their own computer. Any claim that we can
detect that would be false, and worse, it would mean other customers' safety
depended on a check that cannot hold. So we assume every client is modified and
enforce the rules that matter on our side instead: which team a machine may carry
work for, how much data it may use, and what it may be asked to reach.

If you remove the guard from your own copy, the machine may reach your own network
on our instruction. That is your decision about your own network, and it gains you
nothing against anyone else: the destinations still come from PageCrawl, and
PageCrawl only ever issues destinations for your team's monitors.
[SECURITY.md](SECURITY.md) sets out where each rule is enforced and why.

## How it protects the machine it runs on

The reason you can run this at home without handing PageCrawl a route into your
network:

- **It refuses your own network.** Every destination is resolved first, then
  checked, and the connection is made to the exact address that was checked, which
  closes DNS rebinding. Loopback, RFC1918, CGNAT, link-local (including cloud
  metadata) and non-web ports are all refused. `-check` proves this is on.
- **It only carries your monitors.** Each connection is authorised by a credential
  minted for one check and bound to your machine, that monitor and your team.
- **It dials out.** Nothing connects to it, so there is no port to expose.
- **It cannot read anything.** TLS runs between PageCrawl and the site; this moves
  encrypted bytes.

## Layout

| file | role |
|---|---|
| `main.go` | flags, modes, the reconnect loop |
| `config.go` | flag/env/file precedence, saved token |
| `tunnel.go` | websocket to the gateway, stream multiplexing |
| `protocol.go` | the wire format spoken with the gateway |
| `guard.go` | destination validation, the part that protects the operator |
| `doctor.go` | the self-check behind `-check` |
| `ui.go`, `page.go` | the local settings page |
| `state.go` | live status shared by the page and the menu bar |

The gateway speaks the same frame format, and both sides have codec tests covering
partial frames and binary payloads.
