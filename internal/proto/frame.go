// Package proto is the wire format between a tend client and the server.
//
// Frames carry two very different things, and the format reflects that.
// Control messages are JSON, because they are rare, structured, and worth
// being able to read in a hex dump. Pane traffic is raw terminal bytes with an
// 8-byte pane id in front, because it is the hot path: base64 inside JSON
// would cost a third more bytes and an encode on every read from every pane of
// every session.
//
// # Compatibility
//
// A client and server that disagree about the protocol must fail clearly
// rather than subtly. Three rules make that possible, and breaking any of them
// is a protocol break rather than a change:
//
//   - Frame type numbers are permanent. An unknown type is skipped, not
//     guessed at, so a newer peer can send something an older one ignores.
//   - Method names are permanent. An unknown method is answered with an error,
//     never a disconnect, so a missing feature disables one action rather than
//     the whole connection.
//   - New JSON fields are optional. A peer that has never heard of a field
//     must behave as it did before the field existed.
package proto

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

// Version is the protocol this build speaks. It is exchanged in the handshake
// so a mismatch is reported rather than discovered as corrupt data.
const Version = 1

// MaxFrameSize bounds a single frame. Without it a peer sending a bad length
// could make the other side allocate until it dies, which is a denial of
// service that costs one line to prevent.
const MaxFrameSize = 16 << 20

// headerSize is the length prefix: four bytes, big endian, covering the type
// byte and the payload.
const headerSize = 4

// FrameType identifies what a frame carries. The numbers are permanent.
type FrameType uint8

const (
	// FrameRequest is a JSON Request, client to server.
	FrameRequest FrameType = 1
	// FrameResponse is a JSON Response, server to client.
	FrameResponse FrameType = 2
	// FrameEvent is a JSON Event, server to client, unsolicited.
	FrameEvent FrameType = 3
	// FrameInput is keyboard input for a pane, client to server.
	FrameInput FrameType = 4
	// FrameOutput is terminal output from a pane, server to client.
	FrameOutput FrameType = 5
)

func (t FrameType) String() string {
	switch t {
	case FrameRequest:
		return "request"
	case FrameResponse:
		return "response"
	case FrameEvent:
		return "event"
	case FrameInput:
		return "input"
	case FrameOutput:
		return "output"
	default:
		return fmt.Sprintf("unknown(%d)", uint8(t))
	}
}

// Frame is one message on the wire. Payload is owned by the receiver and stays
// valid after the next read.
type Frame struct {
	Type    FrameType
	Payload []byte
}

var (
	// ErrFrameTooLarge means a peer announced more than MaxFrameSize.
	ErrFrameTooLarge = errors.New("proto: frame too large")
	// ErrShortFrame means a frame carried less than its type requires.
	ErrShortFrame = errors.New("proto: frame too short")
)

// Conn reads and writes frames over a connection.
//
// Writes are serialised: the server writes responses, events and pane output
// from different goroutines, and two frames interleaved on the wire would be
// unrecoverable. Reads are not serialised, because exactly one goroutine per
// connection should be reading.
type Conn struct {
	rw io.ReadWriteCloser
	r  *bufio.Reader

	wmu sync.Mutex
	w   *bufio.Writer

	header [headerSize + 1]byte
}

// NewConn wraps a connection.
func NewConn(rw io.ReadWriteCloser) *Conn {
	return &Conn{
		rw: rw,
		r:  bufio.NewReaderSize(rw, 64<<10),
		w:  bufio.NewWriterSize(rw, 64<<10),
	}
}

// ReadFrame reads the next frame.
func (c *Conn) ReadFrame() (Frame, error) {
	var head [headerSize + 1]byte
	if _, err := io.ReadFull(c.r, head[:]); err != nil {
		return Frame{}, err
	}
	length := binary.BigEndian.Uint32(head[:headerSize])
	if length < 1 {
		return Frame{}, ErrShortFrame
	}
	if length > MaxFrameSize {
		return Frame{}, fmt.Errorf("%w: %d bytes", ErrFrameTooLarge, length)
	}

	payload := make([]byte, length-1) // the type byte is already read
	if _, err := io.ReadFull(c.r, payload); err != nil {
		return Frame{}, err
	}
	return Frame{Type: FrameType(head[headerSize]), Payload: payload}, nil
}

// WriteFrame writes a frame and flushes it.
//
// Every frame is flushed rather than batched: a response held in a buffer
// waiting for more traffic is a request that appears to hang, and pane output
// that arrives late is a terminal that feels broken.
func (c *Conn) WriteFrame(t FrameType, payload []byte) error {
	length := len(payload) + 1
	if length > MaxFrameSize {
		return fmt.Errorf("%w: %d bytes", ErrFrameTooLarge, length)
	}

	c.wmu.Lock()
	defer c.wmu.Unlock()

	binary.BigEndian.PutUint32(c.header[:headerSize], uint32(length))
	c.header[headerSize] = byte(t)
	if _, err := c.w.Write(c.header[:]); err != nil {
		return err
	}
	if _, err := c.w.Write(payload); err != nil {
		return err
	}
	return c.w.Flush()
}

// WriteJSON encodes v and writes it as a frame of type t.
func (c *Conn) WriteJSON(t FrameType, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("proto: encoding %s: %w", t, err)
	}
	return c.WriteFrame(t, payload)
}

// WritePaneBytes writes a pane traffic frame.
func (c *Conn) WritePaneBytes(t FrameType, pane uint64, data []byte) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()

	length := len(data) + 1 + 8
	if length > MaxFrameSize {
		return fmt.Errorf("%w: %d bytes", ErrFrameTooLarge, length)
	}

	binary.BigEndian.PutUint32(c.header[:headerSize], uint32(length))
	c.header[headerSize] = byte(t)
	if _, err := c.w.Write(c.header[:]); err != nil {
		return err
	}
	var id [8]byte
	binary.BigEndian.PutUint64(id[:], pane)
	if _, err := c.w.Write(id[:]); err != nil {
		return err
	}
	if _, err := c.w.Write(data); err != nil {
		return err
	}
	return c.w.Flush()
}

// Close closes the underlying connection.
func (c *Conn) Close() error { return c.rw.Close() }

// DecodePaneBytes splits a pane traffic payload into its id and data. The data
// aliases the payload.
func DecodePaneBytes(payload []byte) (pane uint64, data []byte, err error) {
	if len(payload) < 8 {
		return 0, nil, fmt.Errorf("%w: pane frame has %d bytes", ErrShortFrame, len(payload))
	}
	return binary.BigEndian.Uint64(payload[:8]), payload[8:], nil
}

// EncodePaneBytes builds a pane traffic payload. It is the counterpart to
// DecodePaneBytes, for callers not writing through a Conn.
func EncodePaneBytes(pane uint64, data []byte) []byte {
	out := make([]byte, 8+len(data))
	binary.BigEndian.PutUint64(out[:8], pane)
	copy(out[8:], data)
	return out
}
