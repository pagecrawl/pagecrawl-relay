package main

import (
	"encoding/binary"
	"errors"
)

// Wire format, as spoken with the relay gateway:
//
//	[op:u8][streamId:u32 BE][len:u32 BE][payload]
//
// Explicit lengths mean a partial read can never be mistaken for a complete frame,
// which matters because a websocket message boundary is not a frame boundary.
const (
	opOpen   byte = 0x01 // gateway -> client   {"host":..,"port":..}
	opOpenOK byte = 0x02 // client  -> gateway
	opData   byte = 0x03 // both ways           raw bytes
	opClose  byte = 0x04 // both ways
	opError  byte = 0x05 // client  -> gateway  {"message":..}
	opPing   byte = 0x06
	opPong   byte = 0x07
)

const headerBytes = 9

// The length field is 32 bits, so a frame header alone can ask this process to
// allocate ~4 GB. The client runs on a customer's own machine, and the gateway is
// the only thing that can send it frames, so this is the bound that stops a
// hostile or compromised gateway exhausting their memory. Comfortably larger than
// any real chunk: the gateway reads from a socket in 32 KB reads.
const maxFrameBytes = 8 << 20 // 8 MiB

var (
	errShortFrame = errors.New("frame incomplete")
	errOversized  = errors.New("frame exceeds the maximum size")
)

type frame struct {
	op       byte
	streamID uint32
	payload  []byte
}

func encodeFrame(op byte, streamID uint32, payload []byte) []byte {
	out := make([]byte, headerBytes+len(payload))
	out[0] = op
	binary.BigEndian.PutUint32(out[1:5], streamID)
	binary.BigEndian.PutUint32(out[5:9], uint32(len(payload)))
	copy(out[headerBytes:], payload)
	return out
}

// decodeFrame reads one frame from buf, returning it along with the number of bytes
// consumed. It returns errShortFrame when buf does not yet hold a whole frame, so
// the caller keeps buffering instead of acting on a truncated payload.
func decodeFrame(buf []byte) (frame, int, error) {
	if len(buf) < headerBytes {
		return frame{}, 0, errShortFrame
	}

	length := int(binary.BigEndian.Uint32(buf[5:9]))

	// Checked BEFORE the length is used to size anything, so an absurd header can
	// never cause an allocation. The caller treats this as fatal and drops the
	// tunnel rather than trying to resynchronise a stream it cannot parse.
	if length > maxFrameBytes {
		return frame{}, 0, errOversized
	}

	total := headerBytes + length

	if len(buf) < total {
		return frame{}, 0, errShortFrame
	}

	payload := make([]byte, length)
	copy(payload, buf[headerBytes:total])

	return frame{
		op:       buf[0],
		streamID: binary.BigEndian.Uint32(buf[1:5]),
		payload:  payload,
	}, total, nil
}
