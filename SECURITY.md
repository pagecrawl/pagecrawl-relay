# Security

## Reporting a vulnerability

Email **security@pagecrawl.io**. If you would rather not use email, any support
channel reaches us and we will move it somewhere private.

Please include what you did, what happened, and roughly how bad you think it is.
A rough report beats no report; we would much rather read a false alarm than find
out later.

- We reply within **2 working days**, and tell you whether we can reproduce it
  within **5**.
- We will tell you when it is fixed, and credit you in the release notes unless you
  would rather we did not.
- We will not take legal action against anyone who reports a problem in good faith,
  stops at the point of proving it, and does not access or damage other people's
  data while doing so.

Please do not run tests against other customers' relays or against the gateway in
ways that would affect other people. Test against your own machine and your own
account; that is enough to find anything real here.

## What we consider a vulnerability

Ranked by how much we want to hear about it:

1. **Anything that lets one customer's check use another customer's relay**, or
   otherwise crosses the team boundary.
2. **Anything that lets the relay reach the operator's own network**, their
   router, their NAS, cloud metadata, through a name, an address encoding, or a
   redirect the guard does not catch.
3. **Anything that turns the gateway into an open proxy**, or lets an enrolled
   machine take the gateway down.
4. **Any secret that ends up somewhere it should not**: a log, a database column, a
   URL, a crash report.

## Where the boundaries actually are

Being precise about this saves everyone time.

**The client is assumed hostile.** It is open source and it runs on hardware we do
not control, so anyone can patch it. We do not attempt to verify that a running
client is unmodified, because that is not possible for software someone else runs,
and pretending otherwise would be worse than useless: it would put the security of
other customers behind a check that cannot hold.

The gateway must enforce cross-team authorization, usage limits, frame sizes and
resource limits without trusting the client's reports. The client independently
limits destinations, frames, pending dials, streams and queued bytes.

The client trusts the gateway to request the intended checks. It receives a
destination hostname and port, not a separately verifiable proof of team or
monitor ownership. A compromised gateway could request arbitrary permitted public
destinations or consume bandwidth within the client's limits. The local guard
reduces access to private networks; it is not a complete boundary against every
possible routing configuration.

## Content and credentials

HTTPS content remains encrypted between the PageCrawl worker and destination.
Plain HTTP content is unencrypted and visible to the relay host and destination
network. The gateway connection uses WSS by default, which protects that hop.
Hostnames, ports and traffic sizes are visible to the relay even for HTTPS.

Per-check proxy authorization is cached for 20 seconds by default, bounded by
credential expiry. A check finishing or being revoked can therefore remain
accepted briefly from cache. Existing streams have a separate expiry and maximum
five-minute lifetime. Local pause and forget close sockets directly.

The enrolment token is a bearer credential. It travels in the Authorization
header, not a query parameter. Desktop configuration and the per-run settings URL
are stored in the user's config directory with mode 0600 on Unix. The settings
page binds loopback and checks its random key plus any supplied Origin.

## The guard, specifically

`guard.go` resolves each destination and filters refused addresses before dialing
an approved IP directly. There is no second DNS lookup between validation and
connection. A name returning both public and private addresses can use only its
approved public addresses.

Refused address ranges include loopback, RFC1918, CGNAT (100.64/10), unspecified,
multicast, reserved IPv4, link-local (including 169.254.169.254), private IPv6, and
the Azure platform address 168.63.129.16. The complete 6to4 (2002::/16), Teredo
(2001::/32), well-known NAT64 (64:ff9b::/96) and local-use NAT64 (64:ff9b:1::/48)
prefixes are refused. IPv4-mapped IPv6 is checked as its embedded IPv4 address.
Locally configured translation prefixes and public addresses routed to private
services are outside what an address list can reliably identify.

Names `localhost` and suffixes `.local`, `.internal`, `.localdomain`, `.lan` and
`.home` are refused before resolution. Other names and numeric spellings must
resolve to an approved address.

The port denylist is **22, 23, 25, 135, 137, 138, 139, 445, 3389, 5432, 6379,
11211 and 27017**. Other ports from 1 to 65535 are permitted. This is a denylist
of selected services, not an HTTP-only port policy.

The gateway also validates destinations. Its hostname check does not resolve
using the relay's network, so the client's post-resolution check remains essential.

## Resource and lifecycle limits

The client permits 64 stream slots, including pending opens and streams whose
workers are still closing. Destination queues are limited to 4 MiB per stream
and 8 MiB in aggregate; the gateway write queue is limited to 8 MiB. Both directions
also have message-count limits. Writes have 10-second deadlines. A destination
queue overflow closes that stream, and gateway queue overflow closes the tunnel.
Each websocket message and protocol payload is limited to roughly 8 MiB. These
limits bound protocol buffers, not total Go runtime memory or bandwidth.

Pause, forget and token replacement cancel pending DNS/dials and close active
sockets before returning success. Self-check authentication uses a diagnostic
connection that does not register a tunnel or mark the relay online. A successful
websocket upgrade is insufficient: the gateway must acknowledge authentication.

## Scope

**In scope:** this client, the relay protocol, and the way the gateway
authenticates and authorises relays.

**Out of scope:** reports that amount to "a modified client can reach its own
network" (see above), findings against the PageCrawl web application (report those
the same way, they are simply a different component), and automated scanner output
with no demonstrated impact.
