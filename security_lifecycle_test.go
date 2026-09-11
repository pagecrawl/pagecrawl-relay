package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func testConfig(url string) Config {
	return Config{GatewayURL: strings.Replace(url, "http", "ws", 1), Token: strings.Repeat("a", 64),
		DialTimeout: time.Second, IdleTimeout: time.Minute, MaxBackoff: time.Minute}
}

func tunnelPair(t *testing.T) (*tunnel, *websocket.Conn) {
	t.Helper()
	accepted := make(chan *websocket.Conn, 1)
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		accepted <- ws
		<-done
	}))
	t.Cleanup(func() { close(done); srv.Close() })
	client, err := dialContext(context.Background(), testConfig(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client, <-accepted
}

func await(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("operation did not finish")
	}
}

func isolatedConfig(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
}

func TestFrameLengthRejectsUint32Overflow(t *testing.T) {
	for _, size := range []uint32{0x80000000, 0xffffffff} {
		buf := make([]byte, headerBytes)
		binary.BigEndian.PutUint32(buf[5:], size)
		if _, _, err := decodeFrame(buf); err != errOversized {
			t.Fatalf("length %x: %v", size, err)
		}
	}
}

func TestGuardRefusesTranslationAndPlatformAddresses(t *testing.T) {
	for _, host := range []string{
		"64:ff9b::7f00:1", "64:ff9b::808:808", "64:ff9b:1::a00:1",
		"64:ff9b:1:1234::1", "2001:0:4136:e378:8000:63bf:3fff:fdd2",
		"2002:7f00:1::1", "168.63.129.16", "::ffff:168.63.129.16",
	} {
		if _, err := resolveAllowed(host, 443); !Refused(err) {
			t.Errorf("allowed %s: %v", host, err)
		}
	}
	for _, host := range []string{"8.8.8.8", "::ffff:8.8.8.8", "2606:4700:4700::1111"} {
		if _, err := resolveAllowed(host, 443); err != nil {
			t.Errorf("refused public %s: %v", host, err)
		}
	}
}

func TestSettingsChangesCancelSessionAndCloseSockets(t *testing.T) {
	for _, action := range []string{"pause", "forget", "token"} {
		t.Run(action, func(t *testing.T) {
			isolatedConfig(t)
			tun, peer := tunnelPair(t)
			client := newRelayClient(tun.cfg, NewState())
			ctx, _, _, _ := client.session(context.Background())
			if !client.attach(ctx, tun) {
				t.Fatal("attach failed")
			}
			pending := tun.reserveStream(1)
			active := tun.reserveStream(2)
			local, remote := net.Pipe()
			defer remote.Close()
			active.conn = local

			// Exercise simultaneous config/status reads under the race detector.
			var readers sync.WaitGroup
			readers.Add(1)
			go func() {
				defer readers.Done()
				for i := 0; i < 1000; i++ {
					_ = client.config()
					_ = client.state.Snapshot()
				}
			}()
			var err error
			switch action {
			case "pause":
				_, err = client.togglePause()
			case "forget":
				err = client.setToken("")
			case "token":
				err = client.setToken(strings.Repeat("b", 64))
			}
			readers.Wait()
			if err != nil {
				t.Fatal(err)
			}
			if ctx.Err() == nil || pending.ctx.Err() == nil || active.ctx.Err() == nil {
				t.Fatal("a settings change left work running")
			}
			if client.state.Snapshot().Connected {
				t.Fatal("still reported connected")
			}
			_ = remote.SetReadDeadline(time.Now().Add(time.Second))
			if _, err := remote.Read(make([]byte, 1)); err == nil {
				t.Fatal("destination socket remained open")
			}
			_ = peer.SetReadDeadline(time.Now().Add(time.Second))
			if _, _, err := peer.ReadMessage(); err == nil {
				t.Fatal("gateway socket remained open")
			}
		})
	}
}

func TestPendingOpenIsCancelledBeforeDNSCompletes(t *testing.T) {
	tun, _ := tunnelPair(t)
	started := make(chan struct{}, 1)
	oldResolver := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	defer func() { net.DefaultResolver = oldResolver }()
	tun.dispatch(frame{op: opOpen, streamID: 1, payload: []byte(`{"host":"pending.example","port":443}`)})
	await(t, started)
	tun.dispatch(frame{op: opClose, streamID: 1})
	done := make(chan struct{})
	go func() { tun.workers.Wait(); close(done) }()
	await(t, done)
	if len(tun.streams) != 0 || len(tun.openSlots) != 0 {
		t.Fatal("pending open leaked its stream slot")
	}
}

func TestStreamReservationsStayBoundedAfterClose(t *testing.T) {
	tun, _ := tunnelPair(t)
	for i := 0; i < maxStreams; i++ {
		s := tun.reserveStream(uint32(i))
		if s == nil {
			t.Fatal("premature stream limit")
		}
		if tun.reserveStream(uint32(i)) != nil {
			t.Fatal("duplicate stream accepted")
		}
		// Its ID is gone, but its worker has not exited, so its slot must stay used.
		tun.dropStream(s.id, s)
	}
	if tun.reserveStream(999) != nil {
		t.Fatal("close/open churn bypassed the pending worker limit")
	}
}

func TestDestinationQueueIsBounded(t *testing.T) {
	tun, _ := tunnelPair(t)
	s := tun.reserveStream(1)
	local, remote := net.Pipe()
	defer remote.Close()
	s.conn = local
	tun.queueDestination(1, make([]byte, maxStreamQueueBytes))
	if tun.queued != maxStreamQueueBytes {
		t.Fatal("missing queue accounting")
	}
	tun.queueDestination(1, []byte{1})
	if s.ctx.Err() == nil || tun.queued != 0 {
		t.Fatal("overflow did not release the stream")
	}
}

func TestGatewayQueueIsBounded(t *testing.T) {
	tun, _ := tunnelPair(t)
	var err error
	for i := 0; i <= maxQueueMessages; i++ {
		err = tun.send(opData, 1, make([]byte, 32<<10))
		if err != nil {
			break
		}
	}
	if !errors.Is(err, errBackpressure) || tun.ctx.Err() == nil {
		t.Fatal("gateway queue overflow did not close tunnel")
	}
	if tun.writeQueued > maxTunnelQueueBytes {
		t.Fatal("gateway byte budget exceeded")
	}
}

type blockedWriter struct {
	net.Conn
	started chan struct{}
	once    sync.Once
}

func (c *blockedWriter) Write(b []byte) (int, error) {
	c.once.Do(func() { close(c.started) })
	return c.Conn.Write(b)
}

func TestBlockedDestinationDoesNotBlockPingOrClose(t *testing.T) {
	tun, peer := tunnelPair(t)
	s := tun.reserveStream(1)
	local, remote := net.Pipe()
	defer remote.Close()
	conn := &blockedWriter{Conn: local, started: make(chan struct{})}
	s.conn = conn
	writerDone := make(chan struct{})
	go func() { tun.writeDestination(s, conn); close(writerDone) }()
	runDone := make(chan struct{})
	go func() { _ = tun.Run(); close(runDone) }()
	defer func() { tun.Close(); await(t, runDone) }()
	if err := peer.WriteMessage(websocket.BinaryMessage, encodeFrame(opData, 1, []byte("blocked"))); err != nil {
		t.Fatal(err)
	}
	await(t, conn.started)
	if err := peer.WriteMessage(websocket.BinaryMessage, encodeFrame(opPing, 0, nil)); err != nil {
		t.Fatal(err)
	}
	_ = peer.SetReadDeadline(time.Now().Add(time.Second))
	_, data, err := peer.ReadMessage()
	if err != nil {
		t.Fatal("blocked write prevented pong:", err)
	}
	f, _, err := decodeFrame(data)
	if err != nil || f.op != opPong {
		t.Fatalf("wanted pong, got %v, %v", f, err)
	}
	if err := peer.WriteMessage(websocket.BinaryMessage, encodeFrame(opClose, 1, nil)); err != nil {
		t.Fatal(err)
	}
	await(t, writerDone)
}

func TestMalformedFrameClosesGatewaySocket(t *testing.T) {
	tun, peer := tunnelPair(t)
	done := make(chan error, 1)
	go func() { done <- tun.Run() }()
	data := make([]byte, headerBytes)
	binary.BigEndian.PutUint32(data[5:], 0xffffffff)
	if err := peer.WriteMessage(websocket.BinaryMessage, data); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != errOversized {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("malformed tunnel did not terminate")
	}
	_ = peer.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := peer.ReadMessage(); err == nil {
		t.Fatal("Run returned without closing its websocket")
	}
}

func TestSupervisorResumesWithLatestSettings(t *testing.T) {
	isolatedConfig(t)
	type connection struct {
		token  string
		closed chan struct{}
	}
	accepted := make(chan connection, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		entry := connection{token: r.Header.Get("Authorization"), closed: make(chan struct{})}
		accepted <- entry
		_, _, _ = ws.ReadMessage()
		close(entry.closed)
	}))
	defer srv.Close()
	client := newRelayClient(testConfig(srv.URL), NewState())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { supervise(ctx, client); close(done) }()
	defer func() { cancel(); client.stop(); await(t, done) }()
	next := func() connection {
		t.Helper()
		select {
		case entry := <-accepted:
			return entry
		case <-time.After(2 * time.Second):
			t.Fatal("supervisor did not connect")
			return connection{}
		}
	}
	noConnection := func() {
		t.Helper()
		select {
		case <-accepted:
			t.Fatal("paused or forgotten relay reconnected")
		case <-time.After(100 * time.Millisecond):
		}
	}
	first := next()
	if paused, err := client.togglePause(); err != nil || !paused {
		t.Fatal(paused, err)
	}
	await(t, first.closed)
	noConnection()
	if paused, err := client.togglePause(); err != nil || paused {
		t.Fatal(paused, err)
	}
	second := next()
	if err := client.setToken(strings.Repeat("b", 64)); err != nil {
		t.Fatal(err)
	}
	await(t, second.closed)
	third := next()
	if third.token != "Bearer "+strings.Repeat("b", 64) {
		t.Fatal("reconnected with stale token")
	}
	if err := client.setToken(""); err != nil {
		t.Fatal(err)
	}
	await(t, third.closed)
	noConnection()
}

func TestCancelledSessionCannotAttachALateConnection(t *testing.T) {
	isolatedConfig(t)
	tun, _ := tunnelPair(t)
	client := newRelayClient(tun.cfg, NewState())
	ctx, _, _, _ := client.session(context.Background())
	if err := client.setToken(""); err != nil {
		t.Fatal(err)
	}
	if client.attach(ctx, tun) || tun.ctx.Err() == nil {
		t.Fatal("a late dial revived the forgotten session")
	}
}

func TestGuardChecksTranslatedDNSAnswers(t *testing.T) {
	server, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		for {
			buf := make([]byte, 512)
			n, addr, err := server.ReadFrom(buf)
			if err != nil {
				return
			}
			query := buf[:n]
			if n < 17 {
				continue
			}
			// Copy the one DNS question, then answer AAAA with a private NAT64
			// destination. A queries receive an empty successful response.
			end := 12
			for end < n && query[end] != 0 {
				end += int(query[end]) + 1
			}
			end += 5
			if end > n {
				continue
			}
			response := append([]byte(nil), query[:end]...)
			response[2], response[3] = 0x81, 0x80
			for i := 6; i < 12; i++ {
				response[i] = 0
			}
			if binary.BigEndian.Uint16(query[end-4:end-2]) == 28 {
				response[7] = 1
				response = append(response, 0xc0, 0x0c, 0, 28, 0, 1, 0, 0, 0, 1, 0, 16)
				response = append(response, net.ParseIP("64:ff9b:1::c0a8:101").To16()...)
			}
			_, _ = server.WriteTo(response, addr)
		}
	}()
	defer func() { _ = server.Close(); await(t, finished) }()
	oldResolver := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "udp", server.LocalAddr().String())
	}}
	defer func() { net.DefaultResolver = oldResolver }()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := resolveAllowedContext(ctx, "public-looking.example.", 443); !Refused(err) {
		t.Fatal("translated DNS answer was not refused:", err)
	}
}

func TestLargeHTTPBodyFrameReachesResponsiveDestination(t *testing.T) {
	tun, peer := tunnelPair(t)
	s := tun.reserveStream(1)
	local, remote := net.Pipe()
	defer remote.Close()
	s.conn = local
	writerDone := make(chan struct{})
	go func() { tun.writeDestination(s, local); close(writerDone) }()
	runDone := make(chan struct{})
	go func() { _ = tun.Run(); close(runDone) }()
	defer func() { tun.Close(); await(t, runDone); await(t, writerDone) }()

	// The HTTP gateway sends its permitted 2 MiB body and request headers in
	// one DATA frame. A responsive destination must receive that whole request.
	payload := append([]byte("POST /upload HTTP/1.1\r\nHost: example.com\r\nContent-Length: 2097152\r\n\r\n"), bytes.Repeat([]byte("x"), 2<<20)...)
	received := make(chan error, 1)
	go func() {
		data := make([]byte, len(payload))
		_, err := io.ReadFull(remote, data)
		if err == nil && !bytes.Equal(data, payload) {
			err = errors.New("request body changed")
		}
		received <- err
	}()
	if err := peer.WriteMessage(websocket.BinaryMessage, encodeFrame(opData, 1, payload)); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-received:
		if err != nil {
			t.Fatal("large request was refused:", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("large request did not reach destination")
	}
}
