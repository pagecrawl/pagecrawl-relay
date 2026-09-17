package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// Report each diagnostic outcome so a partial failure remains understandable.

// CheckResult is one self-check outcome, worded for the person running the relay.
type CheckResult struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"`
}

const doctorTimeout = 40 * time.Second

// RunDoctor checks, in order, everything a working relay needs, and says in plain
// words what is wrong.
func RunDoctor(cfg Config) []CheckResult {
	return RunDoctorContext(context.Background(), cfg)
}

func RunDoctorContext(parent context.Context, cfg Config) []CheckResult {
	ctx, cancel := context.WithTimeout(parent, doctorTimeout)
	defer cancel()
	var out []CheckResult

	// 1. Is there a token at all?
	if cfg.Token == "" {
		return append(out, CheckResult{
			Name:   "Enrolment token",
			OK:     false,
			Detail: "No token configured.",
			Fix:    "Open PageCrawl, go to Settings -> Relays, add this machine, and paste the token here.",
		})
	}
	out = append(out, CheckResult{Name: "Enrolment token", OK: true, Detail: "Configured."})

	// Separate DNS errors from outbound connection failures.
	endpoint, err := url.Parse(cfg.GatewayURL)
	if err != nil || endpoint.Host == "" {
		return append(out, CheckResult{
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

	if addrs, err := net.DefaultResolver.LookupHost(ctx, host); err != nil {
		out = append(out, CheckResult{
			Name: "DNS", OK: false,
			Detail: fmt.Sprintf("Could not resolve %s: %v", host, err),
			Fix:    "Check this machine's DNS. A filtering resolver or a captive portal will do this.",
		})
	} else {
		out = append(out, CheckResult{Name: "DNS", OK: true, Detail: fmt.Sprintf("%s resolves (%s)", host, strings.Join(addrs, ", "))})
	}

	// Verify outbound TCP independently of token authentication.
	conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		out = append(out, CheckResult{
			Name: "Outbound connection", OK: false,
			Detail: fmt.Sprintf("Could not reach %s:%s: %v", host, port, err),
			Fix:    "This machine must be able to make outbound HTTPS connections. Nothing needs to be opened INBOUND.",
		})
	} else {
		conn.Close()
		out = append(out, CheckResult{Name: "Outbound connection", OK: true, Detail: fmt.Sprintf("Reached %s:%s", host, port)})
	}

	// A probe must acknowledge authentication without replacing the live tunnel.
	out = append(out, gatewayCheck(ctx, cfg))

	// These literal addresses exercise the guard without an external DNS lookup.
	_, loopbackErr := resolveAllowedContext(ctx, "127.0.0.1", 443)
	_, lanErr := resolveAllowedContext(ctx, "192.168.1.1", 443)
	_, publicErr := resolveAllowedContext(ctx, "1.1.1.1", 443)

	guardOK := Refused(loopbackErr) && Refused(lanErr) && publicErr == nil
	out = append(out, CheckResult{
		Name: "Local network protection", OK: guardOK,
		Detail: "Requests to your own network are refused; public sites are allowed.",
	})

	// One outbound request reports the public exit address.
	out = append(out, exitAddressCheck(ctx))

	return out
}

// A websocket upgrade alone does not authenticate the token. Only the explicit
// diagnostic acknowledgement counts; rejection, early close and timeout fail.
func gatewayCheck(ctx context.Context, cfg Config) CheckResult {
	result := CheckResult{Name: "Gateway accepted this machine"}
	probeCtx, cancel := context.WithTimeout(ctx, cfg.DialTimeout)
	defer cancel()
	conn, err := gatewayConnection(probeCtx, cfg, true)
	if err == nil {
		defer conn.Close()
		stop := context.AfterFunc(probeCtx, func() { _ = conn.Close() })
		defer stop()
		conn.SetReadLimit(4096)
		_ = conn.SetReadDeadline(time.Now().Add(cfg.DialTimeout))
		var kind int
		var message []byte
		kind, message, err = conn.ReadMessage()
		if err == nil {
			var ack struct {
				Authenticated bool `json:"authenticated"`
			}
			if kind != websocket.TextMessage || json.Unmarshal(message, &ack) != nil || !ack.Authenticated {
				err = fmt.Errorf("gateway did not acknowledge authentication")
			}
		}
	}
	if err == nil {
		result.OK = true
		result.Detail = "Token valid. The diagnostic did not replace the running tunnel."
	} else if IsRejected(err) {
		result.Detail = "The gateway rejected the token."
		result.Fix = "The token may have been revoked, or the machine removed in Settings -> Relays. Enrol it again."
	} else {
		result.Detail = "Could not verify authentication: " + err.Error()
		result.Fix = "Check the gateway connection and confirm the gateway supports relay diagnostics."
	}
	return result
}

// exitAddressCheck reports the public address this machine egresses from, which is
// the address monitored pages will see when a check is relayed.
func exitAddressCheck(parent context.Context) CheckResult {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.ipify.org", nil)
	if err != nil {
		return CheckResult{Name: "Exit address", OK: false, Detail: "Could not build the request."}
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return CheckResult{
			Name: "Exit address", OK: false,
			Detail: "Could not determine it: " + err.Error(),
			Fix:    "Not fatal. It only means this self-check could not reach the address service.",
		}
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
	ip := strings.TrimSpace(string(body))

	if resp.StatusCode != http.StatusOK || net.ParseIP(ip) == nil {
		return CheckResult{Name: "Exit address", OK: false, Detail: "The address service did not return a valid IP address."}
	}

	return CheckResult{
		Name: "Exit address", OK: true,
		Detail: ip + " - this is the address monitored sites will see.",
	}
}
