package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// Self-check. Someone who cannot tell whether this is working needs one command
// that says so in plain language, and says what to do when it is not.
//
// Every check reports its own outcome rather than stopping at the first failure,
// because the interesting case is usually "three green then one red" and that last
// line is the answer.

type checkResult struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"`
}

func runDoctor(cfg Config, hasToken bool) []checkResult {
	var out []checkResult

	// 1. Is there a token at all?
	if !hasToken {
		return append(out, checkResult{
			Name:   "Enrolment token",
			OK:     false,
			Detail: "No token configured.",
			Fix:    "Open PageCrawl, go to Settings -> Relays, add this machine, and paste the token here.",
		})
	}
	out = append(out, checkResult{Name: "Enrolment token", OK: true, Detail: "Configured."})

	// 2. Does the gateway name resolve? Distinguished from "cannot connect" because
	//    the usual cause is a DNS filter or a captive portal, not a firewall.
	endpoint, err := url.Parse(cfg.GatewayURL)
	if err != nil || endpoint.Host == "" {
		return append(out, checkResult{
			Name: "Gateway address", OK: false,
			Detail: fmt.Sprintf("%q is not a valid URL.", cfg.GatewayURL),
			Fix:    "Expected something like wss://relay.pagecrawl.io/tunnel.",
		})
	}

	host := endpoint.Hostname()
	port := endpoint.Port()
	if port == "" {
		port = "443"
	}

	if addrs, err := net.LookupHost(host); err != nil {
		out = append(out, checkResult{
			Name: "DNS", OK: false,
			Detail: fmt.Sprintf("Could not resolve %s: %v", host, err),
			Fix:    "Check this machine's DNS. A filtering resolver or a captive portal will do this.",
		})
	} else {
		out = append(out, checkResult{Name: "DNS", OK: true, Detail: fmt.Sprintf("%s resolves (%s)", host, strings.Join(addrs, ", "))})
	}

	// 3. Can we open a TCP connection out? This is the check that fails on a network
	//    that blocks outbound 443, which is the single most common corporate case.
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 10*time.Second)
	if err != nil {
		out = append(out, checkResult{
			Name: "Outbound connection", OK: false,
			Detail: fmt.Sprintf("Could not reach %s:%s: %v", host, port, err),
			Fix:    "This machine must be able to make outbound HTTPS connections. Nothing needs to be opened INBOUND.",
		})
	} else {
		conn.Close()
		out = append(out, checkResult{Name: "Outbound connection", OK: true, Detail: fmt.Sprintf("Reached %s:%s", host, port)})
	}

	// 4. The real test: does the gateway accept this token? A bad token closes the
	//    socket with a specific code rather than failing to connect at all.
	dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+cfg.Token)

	probe := *endpoint
	q := probe.Query()
	q.Set("platform", platformName())
	q.Set("version", Version)
	probe.RawQuery = q.Encode()

	wsConn, resp, err := dialer.Dial(probe.String(), headers)
	switch {
	case err == nil:
		wsConn.Close()
		out = append(out, checkResult{Name: "Gateway accepted this machine", OK: true, Detail: "Token valid, tunnel established."})
	case resp != nil && resp.StatusCode == http.StatusUnauthorized:
		out = append(out, checkResult{
			Name: "Gateway accepted this machine", OK: false,
			Detail: "The gateway rejected the token.",
			Fix:    "The token may have been revoked, or the machine removed in Settings -> Relays. Enrol it again.",
		})
	default:
		detail := err.Error()
		if resp != nil {
			detail = resp.Status
		}
		out = append(out, checkResult{
			Name: "Gateway accepted this machine", OK: false,
			Detail: "Could not establish the tunnel: " + detail,
			Fix:    "If the outbound check above passed, this is usually a proxy or TLS-inspecting firewall in the way.",
		})
	}

	// 5. The guard that protects this machine's own network. Cheap to verify and the
	//    thing an operator most deserves proof of.
	_, loopbackErr := resolveAllowed("127.0.0.1", 443)
	_, lanErr := resolveAllowed("192.168.1.1", 443)
	_, publicErr := resolveAllowed("example.com", 443)

	guardOK := Refused(loopbackErr) && Refused(lanErr) && publicErr == nil
	out = append(out, checkResult{
		Name: "Local network protection", OK: guardOK,
		Detail: "Requests to your own network are refused; public sites are allowed.",
	})

	// 6. What the monitored sites will actually see. This is the number people came
	//    for, so it is worth one outbound request.
	out = append(out, exitAddressCheck())

	return out
}

// exitAddressCheck reports the public address this machine egresses from, which is
// the address monitored pages will see when a check is relayed.
func exitAddressCheck() checkResult {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.ipify.org", nil)
	if err != nil {
		return checkResult{Name: "Exit address", OK: false, Detail: "Could not build the request."}
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return checkResult{
			Name: "Exit address", OK: false,
			Detail: "Could not determine it: " + err.Error(),
			Fix:    "Not fatal. It only means this self-check could not reach the address service.",
		}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
	ip := strings.TrimSpace(string(body))

	if ip == "" {
		return checkResult{Name: "Exit address", OK: false, Detail: "No address returned."}
	}

	return checkResult{
		Name: "Exit address", OK: true,
		Detail: ip + " - this is the address monitored sites will see.",
	}
}

// printDoctor renders the checks for a terminal and returns a process exit code.
func printDoctor(results []checkResult) int {
	failed := 0

	fmt.Println()
	for _, r := range results {
		mark := "OK  "
		if !r.OK {
			mark = "FAIL"
			failed++
		}
		fmt.Printf("  [%s] %s\n         %s\n", mark, r.Name, r.Detail)
		if !r.OK && r.Fix != "" {
			fmt.Printf("         -> %s\n", r.Fix)
		}
	}
	fmt.Println()

	if failed == 0 {
		fmt.Println("  Everything checks out. This machine can relay.")
		return 0
	}

	fmt.Printf("  %d check(s) need attention.\n", failed)

	return 1
}
