package main

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func binaryPutUint32(b []byte, v uint32) { binary.BigEndian.PutUint32(b, v) }

// A length-prefixed protocol that mishandles partial frames corrupts traffic
// silently instead of failing loudly, so partial reads get their own cases.
// Mirrors the codec tests in tests/js/relay-gateway.test.js.
func TestFrameRoundTrip(t *testing.T) {
	f, consumed, err := decodeFrame(encodeFrame(opData, 42, []byte("hello relay")))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.op != opData || f.streamID != 42 || string(f.payload) != "hello relay" {
		t.Fatalf("round trip mismatch: %+v", f)
	}
	if consumed != headerBytes+len("hello relay") {
		t.Fatalf("consumed %d bytes, expected %d", consumed, headerBytes+len("hello relay"))
	}
}

func TestEmptyPayload(t *testing.T) {
	f, _, err := decodeFrame(encodeFrame(opClose, 7, nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.op != opClose || f.streamID != 7 || len(f.payload) != 0 {
		t.Fatalf("unexpected frame: %+v", f)
	}
}

func TestPartialFrameIsNotDecoded(t *testing.T) {
	full := encodeFrame(opData, 1, []byte("abcdefghij"))

	for _, size := range []int{0, 4, headerBytes, len(full) - 1} {
		if _, _, err := decodeFrame(full[:size]); err != errShortFrame {
			t.Errorf("a %d-byte prefix must report errShortFrame, got %v", size, err)
		}
	}

	if _, _, err := decodeFrame(full); err != nil {
		t.Fatalf("the complete frame must decode: %v", err)
	}
}

func TestConcatenatedFramesDecodeInOrder(t *testing.T) {
	stream := append(encodeFrame(opData, 1, []byte("first")), encodeFrame(opData, 2, []byte("second"))...)

	first, consumed, err := decodeFrame(stream)
	if err != nil || string(first.payload) != "first" {
		t.Fatalf("first frame: %+v %v", first, err)
	}

	second, _, err := decodeFrame(stream[consumed:])
	if err != nil || string(second.payload) != "second" || second.streamID != 2 {
		t.Fatalf("second frame: %+v %v", second, err)
	}
}

func TestBinaryPayloadWithHeaderLikeBytes(t *testing.T) {
	payload := []byte{0x01, 0x00, 0x00, 0x00, 0x05, 0xff, 0x00}

	f, _, err := decodeFrame(encodeFrame(opData, 9, payload))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(f.payload, payload) {
		t.Fatalf("payload corrupted: %v", f.payload)
	}
}

// decodeFrame must copy, not alias: the caller reuses its read buffer.
func TestDecodedPayloadDoesNotAliasTheBuffer(t *testing.T) {
	buf := encodeFrame(opData, 1, []byte("stable"))

	f, _, err := decodeFrame(buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for i := range buf {
		buf[i] = 0
	}

	if string(f.payload) != "stable" {
		t.Fatalf("payload aliased the source buffer: %q", f.payload)
	}
}

// The client runs inside a customer's network and the gateway is the only thing
// that can send it frames, so an absurd length field must be refused before it
// sizes an allocation.
func TestOversizedFrameIsRefusedBeforeAllocating(t *testing.T) {
	// A header claiming ~4 GB, with no body behind it.
	header := make([]byte, headerBytes)
	header[0] = opData
	header[5], header[6], header[7], header[8] = 0xff, 0xff, 0xff, 0xff

	_, _, err := decodeFrame(header)
	if err != errOversized {
		t.Fatalf("a 4 GB length must be refused as oversized, got %v", err)
	}
}

func TestFrameAtTheLimitIsStillAccepted(t *testing.T) {
	// Exactly at the cap decodes; one byte over does not. Pins the boundary so a
	// later tightening cannot silently start dropping legitimate traffic.
	atLimit := make([]byte, headerBytes)
	atLimit[0] = opData
	binaryPutUint32(atLimit[5:9], uint32(maxFrameBytes))
	if _, _, err := decodeFrame(atLimit); err != errShortFrame {
		t.Fatalf("a frame at the cap should merely be incomplete here, got %v", err)
	}

	overLimit := make([]byte, headerBytes)
	overLimit[0] = opData
	binaryPutUint32(overLimit[5:9], uint32(maxFrameBytes+1))
	if _, _, err := decodeFrame(overLimit); err != errOversized {
		t.Fatalf("one byte over the cap must be refused, got %v", err)
	}
}
