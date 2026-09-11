# PageCrawl Relay client.
#
# Carries the egress for YOUR OWN PageCrawl monitors. It dials out, so the
# container needs no published ports and no inbound firewall rule.
#
#   docker build -t pagecrawl-relay .
#   docker run -d --name pagecrawl-relay --restart unless-stopped \
#     -e PAGECRAWL_RELAY_TOKEN=your-token pagecrawl-relay

FROM golang:1.27.1-alpine3.24 AS build
WORKDIR /src

# Dependencies first, so a code edit does not re-download the module cache.
COPY go.mod go.sum ./
RUN go mod download

COPY *.go settings.html ./
ARG VERSION=docker
# CGO off: a static binary runs on scratch and on any base image, and the client
# needs no C libraries.
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.Version=${VERSION}" -o /out/pagecrawl-relay .

FROM alpine:3.24
# ca-certificates for the TLS connection to the gateway; tzdata so log timestamps
# match the host's clock rather than UTC.
RUN apk add --no-cache ca-certificates tzdata \
 && adduser -D -H -u 10001 relay
USER relay

COPY --from=build /out/pagecrawl-relay /usr/local/bin/pagecrawl-relay

# -headless because a container has no browser to open, and because a missing
# token should stop the container loudly rather than idle looking healthy.
ENTRYPOINT ["/usr/local/bin/pagecrawl-relay", "-headless"]

# The self-check exercises the real path: DNS, outbound TLS, and whether the
# gateway still accepts this machine's token. A revoked token turns the container
# unhealthy instead of silently relaying nothing.
HEALTHCHECK --interval=60s --timeout=45s --start-period=15s --retries=3 \
  CMD ["/usr/local/bin/pagecrawl-relay", "-check"]
