package main

import (
	"net"
	"testing"
)

// The guard is the reason someone can run this on their home network without
// handing PageCrawl a route into it, so every range that must stay unreachable has
// a case here. Mirrors tests/js/relay-gateway.test.js on the gateway side.
func TestIsPrivateIP(t *testing.T) {
	private := []string{
		"127.0.0.1",       // loopback
		"10.1.2.3",        // RFC1918 /8
		"172.16.0.1",      // RFC1918 /12 lower bound
		"172.31.255.254",  // RFC1918 /12 upper bound
		"192.168.1.1",     // the typical home router
		"169.254.169.254", // link-local: cloud metadata
		"0.0.0.0",         // unspecified
		"224.0.0.1",       // multicast
		"100.64.0.1",      // CGNAT, common on home ISPs
		"240.0.0.1",       // reserved
		"::1",             // IPv6 loopback
		"fd00::1",         // IPv6 unique local
		"fe80::1",         // IPv6 link-local
	}

	for _, addr := range private {
		if !isPrivateIP(net.ParseIP(addr)) {
			t.Errorf("%s must be treated as private", addr)
		}
	}

	public := []string{
		"203.0.113.10",
		"8.8.8.8",
		"172.32.0.1",  // just outside RFC1918 /12
		"172.15.0.1",  // just below RFC1918 /12
		"192.167.1.1", // adjacent to, but not, 192.168/16
		"2606:4700::1111",
	}

	for _, addr := range public {
		if isPrivateIP(net.ParseIP(addr)) {
			t.Errorf("%s must be reachable", addr)
		}
	}
}

func TestUnparseableAddressIsRefused(t *testing.T) {
	// Unknown must mean no, never "probably fine".
	if !isPrivateIP(nil) {
		t.Fatal("a nil address must be refused")
	}
}

func TestIsPrivateHostname(t *testing.T) {
	for _, host := range []string{"localhost", "", "printer.local", "nas.internal", "box.lan", "router.home"} {
		if !isPrivateHostname(host) {
			t.Errorf("%q must be treated as local", host)
		}
	}

	for _, host := range []string{"example.com", "www.costco.com", "sub.domain.co.uk"} {
		if isPrivateHostname(host) {
			t.Errorf("%q must be reachable", host)
		}
	}
}

func TestResolveAllowedRefusesBlockedPorts(t *testing.T) {
	for _, port := range []int{22, 25, 445, 3389, 5432, 6379, 27017} {
		if _, err := resolveAllowed("example.com", port); err == nil {
			t.Errorf("port %d must not be proxyable", port)
		}
	}
}

func TestResolveAllowedRefusesOutOfRangePorts(t *testing.T) {
	for _, port := range []int{0, -1, 70000} {
		if _, err := resolveAllowed("example.com", port); err == nil {
			t.Errorf("port %d must be rejected", port)
		}
	}
}

func TestResolveAllowedRefusesPrivateLiterals(t *testing.T) {
	// No DNS involved: a literal private address must be refused outright.
	for _, host := range []string{"192.168.1.1", "127.0.0.1", "169.254.169.254", "10.0.0.5"} {
		if _, err := resolveAllowed(host, 443); err == nil {
			t.Errorf("%s must not be dialable", host)
		}
	}
}

func TestResolveAllowedAcceptsPublicLiteral(t *testing.T) {
	addrs, err := resolveAllowed("203.0.113.10", 443)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(addrs) != 1 || addrs[0].String() != "203.0.113.10" {
		t.Fatalf("expected the literal back, got %v", addrs)
	}
}

func TestResolveAllowedRefusesLocalHostnames(t *testing.T) {
	for _, host := range []string{"localhost", "printer.local", "nas.internal"} {
		if _, err := resolveAllowed(host, 443); err == nil {
			t.Errorf("%q must not be dialable", host)
		}
	}
}
