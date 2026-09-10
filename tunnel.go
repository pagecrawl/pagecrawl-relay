package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type openRequest struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

// tunnel is one connection to the gateway plus every TCP stream flowing over it.
type tunnel struct {
	conn *websocket.Conn
	cfg  Config
	// Optional: the display shared with the settings page and the menu bar.
	state   *State
	writeMu sync.Mutex // gorilla permits only one concurrent writer

	streamsMu sync.Mutex
	streams   map[uint32]net.Conn

	bytesMu sync.Mutex
	bytes   int64
}

// send serialises writes, because every stream shares the one websocket.
func (t *tunnel) send(op byte, streamID uint32, payload []byte) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()

	return t.conn.WriteMessage(websocket.BinaryMessage, encodeFrame(op, streamID, payload))
}

func (t *tunnel) addBytes(n int) {
	t.bytesMu.Lock()
	t.bytes += int64(n)
	t.bytesMu.Unlock()

	if t.state != nil {
		t.state.AddBytes(int64(n))
	}
}

// Bytes reports the traffic carried so far, for the local usage display. The
// authoritative figure for quota is the gateway's, which sees both ends.
func (t *tunnel) Bytes() int64 {
	t.bytesMu.Lock()
	defer t.bytesMu.Unlock()
	return t.bytes
}

func (t *tunnel) setStream(id uint32, conn net.Conn) {
	t.streamsMu.Lock()
	t.streams[id] = conn
	t.streamsMu.Unlock()
}

func (t *tunnel) getStream(id uint32) (net.Conn, bool) {
	t.streamsMu.Lock()
	defer t.streamsMu.Unlock()
	conn, ok := t.streams[id]
	return conn, ok
}

func (t *tunnel) dropStream(id uint32) {
	t.streamsMu.Lock()
	if conn, ok := t.streams[id]; ok {
		delete(t.streams, id)
		conn.Close()
	}
	t.streamsMu.Unlock()
}

func (t *tunnel) closeAllStreams() {
	t.streamsMu.Lock()
	for id, conn := range t.streams {
		conn.Close()
		delete(t.streams, id)
	}
	t.streamsMu.Unlock()
}

// handleOpen resolves and dials a destination on behalf of one check.
//
// This is the point where the machine's owner is protected: resolveAllowed decides
// what may be reached, and the dial targets the exact address it validated.
func (t *tunnel) handleOpen(streamID uint32, payload []byte) {
	var req openRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		t.refuse(streamID, "malformed open request")
		return
	}

	addrs, err := resolveAllowed(req.Host, req.Port)
	if err != nil {
		// Only a policy refusal is worth the operator's attention: that is the guard
		// protecting their network, and they should see it. A name that does not
		// resolve is almost always their own DNS filtering an ad or tracker domain,
		// which happens on most page loads and would otherwise bury the real events.
		if Refused(err) {
			log.Printf("refused %s:%d (%v)", req.Host, req.Port, err)

			if t.state != nil {
				t.state.NoteRefused(req.Host, req.Port, err.Error())
			}
		} else if t.cfg.Verbose {
			log.Printf("could not reach %s:%d (%v)", req.Host, req.Port, err)
		}

		t.refuse(streamID, err.Error())
		return
	}

	var conn net.Conn
	for _, ip := range addrs {
		dialed, dialErr := net.DialTimeout("tcp", net.JoinHostPort(ip.String(), strconv.Itoa(req.Port)), t.cfg.DialTimeout)
		if dialErr == nil {
			conn = dialed
			break
		}
		err = dialErr
	}

	if conn == nil {
		t.refuse(streamID, fmt.Sprintf("dial %s: %v", req.Host, err))
		return
	}

	t.setStream(streamID, conn)

	if t.state != nil {
		t.state.NoteDestination(req.Host, req.Port)
	}

	if sendErr := t.send(opOpenOK, streamID, nil); sendErr != nil {
		t.dropStream(streamID)
		return
	}

	go t.pump(streamID, conn)
}

func (t *tunnel) refuse(streamID uint32, message string) {
	body, _ := json.Marshal(map[string]string{"message": message})
	_ = t.send(opError, streamID, body)
}

// pump copies bytes from the destination back to the gateway until either end closes.
func (t *tunnel) pump(streamID uint32, conn net.Conn) {
	buf := make([]byte, 32*1024)

	for {
		_ = conn.SetReadDeadline(time.Now().Add(t.cfg.IdleTimeout))

		n, err := conn.Read(buf)
		if n > 0 {
			t.addBytes(n)
			if sendErr := t.send(opData, streamID, buf[:n]); sendErr != nil {
				break
			}
		}
		if err != nil {
			break
		}
	}

	t.dropStream(streamID)
	_ = t.send(opClose, streamID, nil)
}

// Keepalive. A relay tunnel is idle between checks, and anything on the path will
// eventually reclaim an idle connection: Cloudflare cuts websockets after roughly
// 100s, and a home router's NAT table is often shorter still. Without traffic the
// tunnel dies silently, the relay reads as offline, and monitors fall back to the
// proxy ladder until the next reconnect.
//
// pingInterval must stay comfortably under the shortest of those timeouts, and
// pongWait must exceed it so one dropped pong does not tear down a healthy tunnel.
const (
	pingInterval = 30 * time.Second
	pongWait     = 75 * time.Second
	writeWait    = 10 * time.Second
)

// keepalive pings until the tunnel closes. Websocket-level control frames rather
// than a protocol opcode, so every middlebox on the path counts them as activity.
func (t *tunnel) keepalive(done <-chan struct{}) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			// WriteControl is safe alongside the other writers (gorilla permits it
			// concurrently), so this does not contend with stream data.
			if err := t.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait)); err != nil {
				return
			}
		}
	}
}

// Run reads frames until the websocket closes, then tears every stream down.
func (t *tunnel) Run() error {
	defer t.closeAllStreams()

	done := make(chan struct{})
	defer close(done)

	go t.keepalive(done)

	// Hard ceiling on a single websocket message, independent of the frame check in
	// decodeFrame: gorilla otherwise buffers a whole message before returning it, so
	// without this a hostile gateway could exhaust memory before any frame is parsed.
	t.conn.SetReadLimit(maxFrameBytes + headerBytes)

	// A missed pong is how a tunnel that died without a FIN gets noticed: the read
	// deadline expires, ReadMessage errors, and main.go reconnects.
	_ = t.conn.SetReadDeadline(time.Now().Add(pongWait))
	t.conn.SetPongHandler(func(string) error {
		return t.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	var buffer []byte

	for {
		_, message, err := t.conn.ReadMessage()
		if err != nil {
			return err
		}

		// Any traffic proves the tunnel is alive, not just pongs.
		_ = t.conn.SetReadDeadline(time.Now().Add(pongWait))

		buffer = append(buffer, message...)

		// Drain every complete frame the buffer now holds; keep the remainder.
		for {
			f, consumed, decodeErr := decodeFrame(buffer)
			if decodeErr == errOversized {
				// Unparseable from here on: the stream cannot be resynchronised, so
				// drop the tunnel and let main.go reconnect.
				return decodeErr
			}
			if decodeErr != nil {
				break
			}
			buffer = buffer[consumed:]
			t.dispatch(f)
		}
	}
}

func (t *tunnel) dispatch(f frame) {
	switch f.op {
	case opOpen:
		go t.handleOpen(f.streamID, f.payload)

	case opData:
		if conn, ok := t.getStream(f.streamID); ok {
			if n, err := conn.Write(f.payload); err != nil {
				t.dropStream(f.streamID)
			} else {
				t.addBytes(n)
			}
		}

	case opClose:
		t.dropStream(f.streamID)

	case opPing:
		_ = t.send(opPong, 0, nil)

	default:
		// Unknown opcode: ignore, so the gateway can add one without a flag day.
	}
}

// dial opens the websocket to the gateway. The client always dials out, so the
// machine needs no port forwarding and no inbound firewall rule.
func dial(cfg Config) (*tunnel, error) {
	endpoint, err := url.Parse(cfg.GatewayURL)
	if err != nil {
		return nil, fmt.Errorf("gateway url: %w", err)
	}

	// Only non-secret metadata goes in the query string. The token travels in a
	// header because a query string is logged verbatim by nginx's default combined
	// format ($request includes the query) and by anything else on the path,
	// Cloudflare included. That would write the machine's credential to disk on
	// every connect and every reconnect.
	query := endpoint.Query()
	query.Set("platform", platformName())
	query.Set("version", Version)
	endpoint.RawQuery = query.Encode()

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+cfg.Token)

	conn, resp, err := websocket.DefaultDialer.Dial(endpoint.String(), headers)
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("gateway refused the connection (%s)", resp.Status)
		}
		return nil, err
	}

	return &tunnel{
		conn:    conn,
		cfg:     cfg,
		streams: make(map[uint32]net.Conn),
	}, nil
}
