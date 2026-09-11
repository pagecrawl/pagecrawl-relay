package main

import (
	"context"
	"encoding/json"
	"errors"
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

const (
	pingInterval        = 30 * time.Second
	pongWait            = 75 * time.Second
	writeWait           = 10 * time.Second
	maxStreams          = 64
	maxStreamQueueBytes = 4 << 20
	maxTunnelQueueBytes = 8 << 20
	maxQueueMessages    = 256
)

var errBackpressure = errors.New("relay write queue full")

type openRequest struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type stream struct {
	id     uint32
	ctx    context.Context
	cancel context.CancelFunc
	conn   net.Conn
	writes chan []byte
	queued int // includes the write currently in progress
}

// tunnel bounds pending dials, sockets and queued bytes in both directions.
// Only the websocket writer writes data frames; destination writers never block
// the websocket reader, which must remain able to process pongs and CLOSE frames.
type tunnel struct {
	conn      *websocket.Conn
	cfg       Config
	state     *State
	ctx       context.Context
	cancel    context.CancelFunc
	closeOnce sync.Once
	workers   sync.WaitGroup

	streamsMu sync.Mutex
	streams   map[uint32]*stream
	openSlots chan struct{}
	closed    bool
	queued    int

	writeMu     sync.Mutex
	writes      chan []byte
	writeQueued int
	bytesMu     sync.Mutex
	bytes       int64
}

func newTunnel(ctx context.Context, conn *websocket.Conn, cfg Config) *tunnel {
	ctx, cancel := context.WithCancel(ctx)
	return &tunnel{conn: conn, cfg: cfg, ctx: ctx, cancel: cancel,
		streams: make(map[uint32]*stream), writes: make(chan []byte, maxQueueMessages),
		openSlots: make(chan struct{}, maxStreams)}
}

// send never waits behind another stream. An exhausted shared budget closes the
// tunnel, releasing its sockets and buffers rather than accumulating goroutines.
func (t *tunnel) send(op byte, id uint32, payload []byte) error {
	t.writeMu.Lock()
	if t.ctx.Err() != nil {
		t.writeMu.Unlock()
		return t.ctx.Err()
	}
	n := headerBytes + len(payload)
	if len(payload) > maxFrameBytes || t.writeQueued+n > maxTunnelQueueBytes || len(t.writes) == cap(t.writes) {
		t.writeMu.Unlock()
		t.Close()
		return errBackpressure
	}
	t.writeQueued += n
	t.writes <- encodeFrame(op, id, payload)
	t.writeMu.Unlock()
	return nil
}

func (t *tunnel) writeGateway() {
	defer t.workers.Done()
	for {
		select {
		case <-t.ctx.Done():
			return
		case data := <-t.writes:
			_ = t.conn.SetWriteDeadline(time.Now().Add(writeWait))
			err := t.conn.WriteMessage(websocket.BinaryMessage, data)
			t.writeMu.Lock()
			t.writeQueued -= len(data)
			t.writeMu.Unlock()
			if err != nil {
				t.Close()
				return
			}
		}
	}
}

func (t *tunnel) addBytes(n int) {
	t.bytesMu.Lock()
	t.bytes += int64(n)
	t.bytesMu.Unlock()
	if t.state != nil {
		t.state.AddBytes(int64(n))
	}
}

func (t *tunnel) Bytes() int64 {
	t.bytesMu.Lock()
	defer t.bytesMu.Unlock()
	return t.bytes
}

// Reservation precedes DNS and counts against the limit. CLOSE can therefore
// cancel an OPEN that is still resolving, and a duplicate ID cannot leak a socket.
func (t *tunnel) reserveStream(id uint32) *stream {
	t.streamsMu.Lock()
	defer t.streamsMu.Unlock()
	if t.closed || t.streams[id] != nil || len(t.streams) >= maxStreams {
		return nil
	}
	// Keep the slot until its goroutines exit, even if CLOSE removes the ID.
	select {
	case t.openSlots <- struct{}{}:
	default:
		return nil
	}
	ctx, cancel := context.WithCancel(t.ctx)
	s := &stream{id: id, ctx: ctx, cancel: cancel, writes: make(chan []byte, maxQueueMessages)}
	t.streams[id] = s
	return s
}

func (t *tunnel) dropStream(id uint32, expected *stream) bool {
	t.streamsMu.Lock()
	defer t.streamsMu.Unlock()
	s := t.streams[id]
	if s == nil || (expected != nil && s != expected) {
		return false
	}
	delete(t.streams, id)
	t.queued -= s.queued
	s.cancel()
	if s.conn != nil {
		_ = s.conn.Close()
	}
	return true
}

// Close is safe from the settings handler, a writer, or Run's cleanup.
func (t *tunnel) Close() {
	t.closeOnce.Do(func() {
		t.cancel()
		_ = t.conn.Close()
		t.streamsMu.Lock()
		defer t.streamsMu.Unlock()
		t.closed = true
		for id, s := range t.streams {
			s.cancel()
			if s.conn != nil {
				_ = s.conn.Close()
			}
			delete(t.streams, id)
		}
		t.queued = 0
	})
}

func (t *tunnel) handleOpen(s *stream, payload []byte) {
	defer t.workers.Done()
	defer func() { <-t.openSlots }()
	var req openRequest
	if json.Unmarshal(payload, &req) != nil {
		t.failStream(s, "malformed open request")
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, t.cfg.DialTimeout)
	defer cancel()
	addrs, err := resolveAllowedContext(ctx, req.Host, req.Port)
	if err != nil {
		if Refused(err) {
			log.Printf("refused %s:%d (%v)", req.Host, req.Port, err)
			if t.state != nil {
				t.state.NoteRefused(req.Host, req.Port, err.Error())
			}
		} else if t.cfg.Verbose {
			log.Printf("could not reach %s:%d (%v)", req.Host, req.Port, err)
		}
		t.failStream(s, err.Error())
		return
	}

	var conn net.Conn
	for _, ip := range addrs {
		conn, err = (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), strconv.Itoa(req.Port)))
		if err == nil {
			break
		}
	}
	if conn == nil {
		t.failStream(s, fmt.Sprintf("dial %s: %v", req.Host, err))
		return
	}

	t.streamsMu.Lock()
	if t.closed || t.streams[s.id] != s || s.ctx.Err() != nil {
		t.streamsMu.Unlock()
		_ = conn.Close()
		return
	}
	s.conn = conn
	t.streamsMu.Unlock()
	if t.state != nil {
		t.state.NoteDestination(req.Host, req.Port)
	}
	if t.send(opOpenOK, s.id, nil) != nil {
		t.dropStream(s.id, s)
		return
	}
	pumped := make(chan struct{})
	go func() {
		defer close(pumped)
		t.pump(s, conn)
	}()
	t.writeDestination(s, conn)
	<-pumped
}

func (t *tunnel) refuse(id uint32, message string) {
	body, _ := json.Marshal(map[string]string{"message": message})
	_ = t.send(opError, id, body)
}

func (t *tunnel) failStream(s *stream, message string) {
	if t.dropStream(s.id, s) {
		t.refuse(s.id, message)
	}
}

func (t *tunnel) finishStream(s *stream) {
	if t.dropStream(s.id, s) {
		_ = t.send(opClose, s.id, nil)
	}
}

func (t *tunnel) queueDestination(id uint32, data []byte) {
	t.streamsMu.Lock()
	s := t.streams[id]
	if s == nil || s.conn == nil {
		t.streamsMu.Unlock()
		return
	}
	if s.queued+len(data) > maxStreamQueueBytes || t.queued+len(data) > maxTunnelQueueBytes || len(s.writes) == cap(s.writes) {
		t.streamsMu.Unlock()
		t.failStream(s, "destination write queue full")
		return
	}
	s.queued += len(data)
	t.queued += len(data)
	s.writes <- data
	t.streamsMu.Unlock()
}

func (t *tunnel) writeDestination(s *stream, conn net.Conn) {
	defer t.finishStream(s)
	for {
		select {
		case <-s.ctx.Done():
			return
		case data := <-s.writes:
			_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
			n, err := conn.Write(data)
			t.addBytes(n)
			t.streamsMu.Lock()
			if t.streams[s.id] == s {
				s.queued -= len(data)
				t.queued -= len(data)
			}
			t.streamsMu.Unlock()
			if err != nil || n != len(data) {
				return
			}
		}
	}
}

func (t *tunnel) pump(s *stream, conn net.Conn) {
	defer t.finishStream(s)
	buf := make([]byte, 32*1024)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(t.cfg.IdleTimeout))
		n, err := conn.Read(buf)
		if n > 0 {
			t.addBytes(n)
			if t.send(opData, s.id, buf[:n]) != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func (t *tunnel) keepalive() {
	defer t.workers.Done()
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-t.ctx.Done():
			return
		case <-ticker.C:
			// Control writes are safe alongside the single data writer.
			if t.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeWait)) != nil {
				t.Close()
				return
			}
		}
	}
}

func (t *tunnel) Run() error {
	stop := context.AfterFunc(t.ctx, t.Close)
	defer stop()
	defer func() { t.Close(); t.workers.Wait() }()
	t.workers.Add(2)
	go t.writeGateway()
	go t.keepalive()
	t.conn.SetReadLimit(maxFrameBytes + headerBytes)
	_ = t.conn.SetReadDeadline(time.Now().Add(pongWait))
	t.conn.SetPongHandler(func(string) error {
		return t.conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	var buffer []byte
	for {
		kind, message, err := t.conn.ReadMessage()
		if err != nil {
			return err
		}
		if kind != websocket.BinaryMessage {
			return errors.New("expected a binary relay frame")
		}
		_ = t.conn.SetReadDeadline(time.Now().Add(pongWait))
		// A partial frame plus one message is bounded before appending.
		if len(buffer)+len(message) > 2*(maxFrameBytes+headerBytes) {
			return errOversized
		}
		buffer = append(buffer, message...)
		for {
			f, consumed, err := decodeFrame(buffer)
			if err == errOversized {
				return err
			}
			if err != nil {
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
		if len(f.payload) > 4096 {
			t.refuse(f.streamID, "open request too large")
			return
		}
		s := t.reserveStream(f.streamID)
		if s == nil {
			t.refuse(f.streamID, "duplicate stream or stream limit reached")
			return
		}
		t.workers.Add(1)
		go t.handleOpen(s, f.payload)
	case opData:
		t.queueDestination(f.streamID, f.payload)
	case opClose:
		t.dropStream(f.streamID, nil)
	case opPing:
		_ = t.send(opPong, 0, nil)
	}
}

// The same authenticated endpoint serves diagnostics, without registering a
// second tunnel. The bearer credential stays in the header, never the URL.
func gatewayConnection(ctx context.Context, cfg Config, diagnostic bool) (*websocket.Conn, error) {
	endpoint, err := url.Parse(cfg.GatewayURL)
	if err != nil {
		return nil, fmt.Errorf("gateway url: %w", err)
	}
	query := endpoint.Query()
	query.Set("platform", platformName())
	query.Set("version", Version)
	query.Del("diagnostic")
	if diagnostic {
		query.Set("diagnostic", "1")
	}
	endpoint.RawQuery = query.Encode()
	headers := http.Header{"Authorization": {"Bearer " + cfg.Token}}
	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = cfg.DialTimeout
	conn, resp, err := dialer.DialContext(ctx, endpoint.String(), headers)
	if err != nil && resp != nil {
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
		if resp.StatusCode == http.StatusUnauthorized {
			return nil, &websocket.CloseError{Code: closeUnauthorized, Text: "Unauthorized"}
		}
		return nil, fmt.Errorf("gateway refused the connection (%s)", resp.Status)
	}
	return conn, err
}

func dialContext(ctx context.Context, cfg Config) (*tunnel, error) {
	conn, err := gatewayConnection(ctx, cfg, false)
	if err != nil {
		return nil, err
	}
	return newTunnel(ctx, conn, cfg), nil
}
