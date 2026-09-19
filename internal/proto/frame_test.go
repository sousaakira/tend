package proto

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
)

// pipeConn is an in-memory ReadWriteCloser pair, so the codec is tested
// without a socket.
type pipeConn struct {
	io.Reader
	io.Writer
	closed bool
}

func (p *pipeConn) Close() error { p.closed = true; return nil }

// loopback returns a Conn writing into a buffer that the same Conn reads back,
// which is enough to prove a frame survives a round trip.
func loopback() (*Conn, *bytes.Buffer) {
	var buf bytes.Buffer
	return NewConn(&pipeConn{Reader: &buf, Writer: &buf}), &buf
}

func TestFrameRoundTrip(t *testing.T) {
	cases := []struct {
		name    string
		typ     FrameType
		payload []byte
	}{
		{"request", FrameRequest, []byte(`{"id":1,"method":"hello"}`)},
		{"event", FrameEvent, []byte(`{"kind":"pane-state"}`)},
		{"empty payload", FrameResponse, []byte{}},
		{"binary", FrameOutput, []byte{0x00, 0x1b, 0x5b, 0xff, 0xfe, 0x00}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			conn, _ := loopback()
			if err := conn.WriteFrame(c.typ, c.payload); err != nil {
				t.Fatalf("WriteFrame: %v", err)
			}
			got, err := conn.ReadFrame()
			if err != nil {
				t.Fatalf("ReadFrame: %v", err)
			}
			if got.Type != c.typ {
				t.Errorf("type = %v, want %v", got.Type, c.typ)
			}
			if !bytes.Equal(got.Payload, c.payload) {
				t.Errorf("payload = %q, want %q", got.Payload, c.payload)
			}
		})
	}
}

// TestFramesAreSelfDelimiting: frames must survive being written back to back,
// since a socket delivers them as one stream with no boundaries of its own.
func TestFramesAreSelfDelimiting(t *testing.T) {
	conn, _ := loopback()
	payloads := [][]byte{
		[]byte("first"),
		{},
		[]byte("a much longer third payload that spans further"),
		{0xff},
	}
	for _, p := range payloads {
		if err := conn.WriteFrame(FrameEvent, p); err != nil {
			t.Fatal(err)
		}
	}
	for i, want := range payloads {
		got, err := conn.ReadFrame()
		if err != nil {
			t.Fatalf("frame %d: %v", i, err)
		}
		if !bytes.Equal(got.Payload, want) {
			t.Errorf("frame %d = %q, want %q", i, got.Payload, want)
		}
	}
}

func TestPaneBytesRoundTrip(t *testing.T) {
	conn, _ := loopback()
	data := []byte("\x1b[1;31mred\x1b[0m")

	if err := conn.WritePaneBytes(FrameOutput, 42, data); err != nil {
		t.Fatalf("WritePaneBytes: %v", err)
	}
	frame, err := conn.ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	if frame.Type != FrameOutput {
		t.Errorf("type = %v", frame.Type)
	}
	pane, got, err := DecodePaneBytes(frame.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if pane != 42 {
		t.Errorf("pane = %d, want 42", pane)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("data = %q, want %q", got, data)
	}
}

// TestPaneBytesCarriesArbitraryBinary is why pane traffic is not JSON: it must
// pass every byte through untouched, including NULs and invalid UTF-8.
func TestPaneBytesCarriesArbitraryBinary(t *testing.T) {
	data := make([]byte, 256)
	for i := range data {
		data[i] = byte(i)
	}
	conn, _ := loopback()
	if err := conn.WritePaneBytes(FrameOutput, 1, data); err != nil {
		t.Fatal(err)
	}
	frame, err := conn.ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	_, got, err := DecodePaneBytes(frame.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Error("a byte was altered in transit")
	}
}

func TestEncodeDecodePaneBytes(t *testing.T) {
	payload := EncodePaneBytes(7, []byte("hi"))
	pane, data, err := DecodePaneBytes(payload)
	if err != nil {
		t.Fatal(err)
	}
	if pane != 7 || string(data) != "hi" {
		t.Errorf("got pane %d data %q", pane, data)
	}
}

func TestDecodePaneBytesRejectsShortPayload(t *testing.T) {
	for _, payload := range [][]byte{nil, {1}, {1, 2, 3, 4, 5, 6, 7}} {
		if _, _, err := DecodePaneBytes(payload); !errors.Is(err, ErrShortFrame) {
			t.Errorf("payload of %d bytes: err = %v, want ErrShortFrame", len(payload), err)
		}
	}
}

// TestReadFrameRejectsOversizedLength guards against a peer announcing a huge
// frame to make this side allocate until it dies.
func TestReadFrameRejectsOversizedLength(t *testing.T) {
	var buf bytes.Buffer
	var head [5]byte
	binary.BigEndian.PutUint32(head[:4], MaxFrameSize+1)
	head[4] = byte(FrameEvent)
	buf.Write(head[:])

	conn := NewConn(&pipeConn{Reader: &buf, Writer: io.Discard})
	if _, err := conn.ReadFrame(); !errors.Is(err, ErrFrameTooLarge) {
		t.Errorf("err = %v, want ErrFrameTooLarge", err)
	}
}

func TestReadFrameRejectsZeroLength(t *testing.T) {
	var buf bytes.Buffer
	var head [5]byte
	binary.BigEndian.PutUint32(head[:4], 0)
	buf.Write(head[:])

	conn := NewConn(&pipeConn{Reader: &buf, Writer: io.Discard})
	if _, err := conn.ReadFrame(); !errors.Is(err, ErrShortFrame) {
		t.Errorf("err = %v, want ErrShortFrame", err)
	}
}

func TestWriteFrameRejectsOversizedPayload(t *testing.T) {
	conn := NewConn(&pipeConn{Reader: bytes.NewReader(nil), Writer: io.Discard})
	if err := conn.WriteFrame(FrameEvent, make([]byte, MaxFrameSize+1)); !errors.Is(err, ErrFrameTooLarge) {
		t.Errorf("err = %v, want ErrFrameTooLarge", err)
	}
}

func TestReadFrameOnTruncatedStream(t *testing.T) {
	var full bytes.Buffer
	conn := NewConn(&pipeConn{Reader: &full, Writer: &full})
	if err := conn.WriteFrame(FrameEvent, []byte("payload")); err != nil {
		t.Fatal(err)
	}
	// Cut the stream mid-payload.
	truncated := full.Bytes()[:full.Len()-3]
	cut := NewConn(&pipeConn{Reader: bytes.NewReader(truncated), Writer: io.Discard})
	if _, err := cut.ReadFrame(); err == nil {
		t.Error("a truncated frame should fail rather than return short data")
	}
}

// TestUnknownFrameTypeSurvives: a newer peer may send a type this build has
// never heard of, and the reader must hand it over intact rather than failing
// the connection.
func TestUnknownFrameTypeSurvives(t *testing.T) {
	conn, _ := loopback()
	if err := conn.WriteFrame(FrameType(99), []byte("from the future")); err != nil {
		t.Fatal(err)
	}
	got, err := conn.ReadFrame()
	if err != nil {
		t.Fatalf("an unknown frame type should read cleanly: %v", err)
	}
	if got.Type != 99 {
		t.Errorf("type = %v, want 99", got.Type)
	}
	if string(got.Payload) != "from the future" {
		t.Errorf("payload = %q", got.Payload)
	}
	if !strings.Contains(got.Type.String(), "unknown") {
		t.Errorf("String() = %q, want it to say unknown", got.Type.String())
	}
}

// TestConcurrentWritesDoNotInterleave is the reason writes are serialised: the
// server writes responses, events and pane output from different goroutines,
// and two frames spliced together would be unrecoverable.
func TestConcurrentWritesDoNotInterleave(t *testing.T) {
	var buf syncBuffer
	conn := NewConn(&pipeConn{Reader: bytes.NewReader(nil), Writer: &buf})

	const writers = 8
	const each = 50
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			payload := bytes.Repeat([]byte{byte('a' + w)}, 64)
			for i := 0; i < each; i++ {
				if err := conn.WriteFrame(FrameOutput, payload); err != nil {
					t.Errorf("write: %v", err)
					return
				}
			}
		}(w)
	}
	wg.Wait()

	// Every frame must read back whole, with a payload of one repeated byte.
	reader := NewConn(&pipeConn{Reader: bytes.NewReader(buf.Bytes()), Writer: io.Discard})
	count := 0
	for {
		frame, err := reader.ReadFrame()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("frame %d: %v", count, err)
		}
		if len(frame.Payload) != 64 {
			t.Fatalf("frame %d has %d bytes, want 64", count, len(frame.Payload))
		}
		for _, b := range frame.Payload {
			if b != frame.Payload[0] {
				t.Fatalf("frame %d is spliced from two writers", count)
			}
		}
		count++
	}
	if count != writers*each {
		t.Errorf("read %d frames, want %d", count, writers*each)
	}
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *syncBuffer) Bytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.buf.Bytes()...)
}

func TestWriteJSON(t *testing.T) {
	conn, _ := loopback()
	want := Request{ID: 7, Method: MethodHello}
	if err := conn.WriteJSON(FrameRequest, want); err != nil {
		t.Fatal(err)
	}
	frame, err := conn.ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	var got Request
	if err := json.Unmarshal(frame.Payload, &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != want.ID || got.Method != want.Method {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestConnClose(t *testing.T) {
	rw := &pipeConn{Reader: bytes.NewReader(nil), Writer: io.Discard}
	conn := NewConn(rw)
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if !rw.closed {
		t.Error("Close should close the underlying connection")
	}
}

// TestNewJSONFieldsAreOptional: a peer that has never heard of a field must
// behave as it did before the field existed, which is what lets the protocol
// grow without a version bump.
func TestNewJSONFieldsAreOptional(t *testing.T) {
	withExtra := []byte(`{"id":3,"method":"pane.close","params":{"pane":9},"future_field":"ignored"}`)
	var req Request
	if err := json.Unmarshal(withExtra, &req); err != nil {
		t.Fatalf("an unknown field should be ignored, not fail: %v", err)
	}
	if req.Method != MethodPaneClose {
		t.Errorf("method = %q", req.Method)
	}

	// And a message missing everything optional still decodes.
	var minimal Request
	if err := json.Unmarshal([]byte(`{"method":"hello"}`), &minimal); err != nil {
		t.Fatal(err)
	}
	if minimal.ID != 0 || minimal.Method != MethodHello {
		t.Errorf("got %+v", minimal)
	}
}

func BenchmarkWritePaneBytes(b *testing.B) {
	conn := NewConn(&pipeConn{Reader: bytes.NewReader(nil), Writer: io.Discard})
	data := bytes.Repeat([]byte("terminal output line\r\n"), 32)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		if err := conn.WritePaneBytes(FrameOutput, 1, data); err != nil {
			b.Fatal(err)
		}
	}
}
