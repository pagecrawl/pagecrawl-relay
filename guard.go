package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// A policy refusal is worth showing the operator. Ordinary DNS failures can be
// routine filtering of ads or trackers and are logged only in verbose mode.
var errRefused = errors.New("destination refused by policy")

// Refused reports whether err is a policy refusal rather than a lookup failure.
func Refused(err error) bool { return errors.Is(err, errRefused) }

// Selected service ports are denied on this machine, independently of the gateway.
// This is a denylist, not an HTTP-only port policy.
var blockedPorts = map[int]bool{
	22: true, 23: true, 25: true, 135: true, 137: true, 138: true, 139: true,
	445: true, 3389: true, 5432: true, 6379: true, 11211: true, 27017: true,
}

// Translation/tunnel prefixes can reach IPv4 networks hidden inside an IPv6
// address. Refuse the whole prefix, including locally assigned NAT64 addresses.
var blockedNetworks = []netip.Prefix{
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2001::/32"),        // Teredo
	netip.MustParsePrefix("2002::/16"),        // 6to4
	netip.MustParsePrefix("168.63.129.16/32"), // Azure platform services
}

// isPrivateHostname catches the names that never need to leave the machine, before
// any DNS lookup happens.
func isPrivateHostname(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(host, "."))

	if h == "localhost" || h == "" {
		return true
	}

	for _, suffix := range []string{".local", ".internal", ".localdomain", ".home", ".lan"} {
		if strings.HasSuffix(h, suffix) {
			return true
		}
	}

	return false
}

// isPrivateIP reports whether an address is one the relay must never connect to:
// the operator's own LAN, loopback, link-local (including cloud metadata at
// 169.254.169.254), and the various reserved ranges.
func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return true // Unparseable means unknown, and unknown means no.
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	for _, prefix := range blockedNetworks {
		if prefix.Contains(addr.Unmap()) {
			return true
		}
	}

	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}

	// Ranges Go's helpers do not classify but which must not be reachable.
	if v4 := ip.To4(); v4 != nil {
		switch {
		case v4[0] == 0: // "this network"
			return true
		case v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127: // CGNAT, common on home ISPs
			return true
		case v4[0] >= 240: // reserved
			return true
		}
	}

	return false
}

// resolveAllowed resolves host and returns the addresses that are safe to dial.
//
// The check is deliberately performed on the RESOLVED addresses rather than on the
// hostname, and the caller then dials one of the returned IPs directly. A name that
// resolves to 192.168.1.1 is refused even though the name itself looks public,
// which is what closes DNS rebinding: the address we validated is the address we
// connect to, with no second lookup in between.
func resolveAllowed(host string, port int) ([]net.IP, error) {
	return resolveAllowedContext(context.Background(), host, port)
}

func resolveAllowedContext(ctx context.Context, host string, port int) ([]net.IP, error) {
	if port <= 0 || port > 65535 {
		return nil, fmt.Errorf("%w: port %d out of range", errRefused, port)
	}

	if blockedPorts[port] {
		return nil, fmt.Errorf("%w: port %d is not proxyable", errRefused, port)
	}

	if isPrivateHostname(host) {
		return nil, fmt.Errorf("%w: host %q is local", errRefused, host)
	}

	// A literal IP still has to pass; it just needs no lookup.
	if literal := net.ParseIP(host); literal != nil {
		if isPrivateIP(literal) {
			return nil, fmt.Errorf("%w: address %s is private", errRefused, literal)
		}
		return []net.IP{literal}, nil
	}

	resolved, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve %q: %w", host, err)
	}

	allowed := make([]net.IP, 0, len(resolved))
	for _, addr := range resolved {
		if !isPrivateIP(addr.IP) {
			allowed = append(allowed, addr.IP)
		}
	}

	if len(allowed) == 0 {
		// Every answer was private. Refuse rather than trying the next record: this
		// is the shape a rebinding attack takes.
		return nil, fmt.Errorf("%w: host %q resolves only to private addresses", errRefused, host)
	}

	return allowed, nil
}
