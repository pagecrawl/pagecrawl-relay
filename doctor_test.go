package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestDiagnosticRequiresAuthenticationAcknowledgement(t *testing.T) {
	for _, outcome := range []string{"ack", "rejected", "missing", "close", "false", "invalid", "timeout", "http401"} {
		t.Run(outcome, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("diagnostic") != "1" || r.Header.Get("Authorization") != "Bearer "+strings.Repeat("a", 64) {
					t.Error("probe did not use the authenticated diagnostic endpoint")
				}
				if r.URL.Query().Get("token") != "" {
					t.Error("credential leaked to URL")
				}
				if outcome == "http401" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer ws.Close()
				switch outcome {
				case "ack":
					_ = ws.WriteJSON(map[string]bool{"authenticated": true})
				case "rejected":
					_ = ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(closeUnauthorized, "revoked"), time.Now().Add(time.Second))
				case "missing":
					_ = ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(closeMissingToken, "missing"), time.Now().Add(time.Second))
				case "close":
					_ = ws.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(1000, ""), time.Now().Add(time.Second))
				case "false":
					_ = ws.WriteJSON(map[string]bool{"authenticated": false})
				case "invalid":
					_ = ws.WriteMessage(websocket.TextMessage, []byte("accepted"))
				case "timeout":
					_, _, _ = ws.ReadMessage()
				}
			}))
			defer srv.Close()
			cfg := testConfig(srv.URL)
			cfg.DialTimeout = 100 * time.Millisecond
			result := gatewayCheck(context.Background(), cfg)
			if result.OK != (outcome == "ack") {
				t.Fatalf("%s: %+v", outcome, result)
			}
			if (outcome == "rejected" || outcome == "missing" || outcome == "http401") && !strings.Contains(result.Detail, "rejected") {
				t.Fatalf("rejection not explained: %+v", result)
			}
		})
	}
}

func TestDiagnosticDoesNotReconnectTheRunningTunnel(t *testing.T) {
	live := make(chan *websocket.Conn, 1)
	closed := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		if r.URL.Query().Get("diagnostic") == "1" {
			_ = ws.WriteJSON(map[string]bool{"authenticated": true})
			return
		}
		live <- ws
		_, _, _ = ws.ReadMessage()
		close(closed)
	}))
	defer srv.Close()
	cfg := testConfig(srv.URL)
	tun, err := dialContext(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer tun.Close()
	<-live
	if result := gatewayCheck(context.Background(), cfg); !result.OK {
		t.Fatal(result)
	}
	select {
	case <-closed:
		t.Fatal("probe closed the live tunnel")
	default:
	}
	tun.Close()
	await(t, closed)
}
