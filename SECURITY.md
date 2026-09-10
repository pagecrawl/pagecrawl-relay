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
2. **Anything that lets the relay reach the operator's own network** — their
   router, their NAS, cloud metadata — through a name, an address encoding, or a
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

Everything that must be true is therefore enforced on our side:

| Rule | Where it is enforced | Why not the client |
|---|---|---|
| A relay only carries its own team's checks | Gateway, per connection, against a credential minted for one check | A patched client could claim anything |
| Monthly data limits | Gateway counts the bytes it moves | A client could under-report |
| Which sites a check may reach | Only the gateway sends destinations; the client cannot choose targets | A client that picks its own targets would be a proxy for its operator, not a relay |
| Frame and stream limits | Gateway | A client can send whatever it likes |

**What a modified client can do is hurt itself.** Removing the local guard means
that machine may reach its own network on our instruction, which is a decision its
operator has made about their own network. It gains nothing against anyone else:
the destination still comes from the gateway, and the gateway only issues
destinations for that team's monitors.

**What a modified client cannot do:** use another team's relay, obtain other
customers' traffic, use the tunnel to carry its own traffic, exceed its data limit,
or exhaust the gateway.

## The guard, specifically

`guard.go` is the file worth reading. The design point that matters: the
destination is resolved **first**, the resolved address is checked, and the
connection is then made to that exact address. There is no second lookup between
the check and the connection, which is what closes DNS rebinding.

Refused: loopback, RFC1918, CGNAT (100.64/10), link-local including cloud metadata
(169.254.169.254), unique-local and link-local IPv6, IPv4-mapped IPv6, 6to4,
`.local` / `.internal` / `.lan` / `.home`, ambiguous numeric address forms (octal,
hex, dword), and non-web ports.

The gateway enforces the same list independently, so both would have to be wrong.

## Scope

**In scope:** this client, the relay protocol, and the way the gateway
authenticates and authorises relays.

**Out of scope:** reports that amount to "a modified client can reach its own
network" (see above), findings against the PageCrawl web application (report those
the same way, they are simply a different component), and automated scanner output
with no demonstrated impact.
