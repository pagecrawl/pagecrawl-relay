package main

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

// errRefused marks a destination the guard deliberately would not reach: a private
// address, a local name, or a port that is not proxyable.
//
// Distinguished from a resolution or dial failure because the two mean opposite
// things to whoever runs the machine. A refusal is the guard doing its job and is
// worth seeing; a name that does not resolve is usually their own DNS filtering an
// ad or tracker domain, which is routine and would otherwise fill the log with
// lines that look like the guard is over-blocking.
var errRefused = errors.New("destination refused by policy")

// Refused reports whether err is a policy refusal rather than a lookup failure.
func Refused(err error) bool { return errors.Is(err, errRefused) }

// blockedPorts are ports a web page never legitimately loads over. Proxying them
// would turn the relay into a way to reach services on the operator's network.
//
// The relay gateway enforces the same list independently. This copy is the one
// that matters to whoever runs this machine: it is applied here, on their
// hardware, after resolution and before any connection is made.
var blockedPorts = map[int]bool{
	22: true, 23: true, 25: true, 135: true, 137: true, 138: true, 139: true,
	445: true, 3389: true, 5432: true, 6379: true, 11211: true, 27017: true,
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

	resolved, err := net.LookupIP(host)
	if err != nil {
		return nil, fmt.Errorf("resolve %q: %w", host, err)
	}

	allowed := make([]net.IP, 0, len(resolved))
	for _, ip := range resolved {
		if !isPrivateIP(ip) {
			allowed = append(allowed, ip)
		}
	}

	if len(allowed) == 0 {
		// Every answer was private. Refuse rather than trying the next record: this
		// is the shape a rebinding attack takes.
		return nil, fmt.Errorf("%w: host %q resolves only to private addresses", errRefused, host)
	}

	return allowed, nil
}
