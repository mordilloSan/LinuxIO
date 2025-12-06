package ipc

import (
	"fmt"
	"io"
	"net"
)

// StreamReader provides streaming message reading from a persistent connection
type StreamReader struct {
	conn net.Conn
}

// NewStreamReader creates a streaming reader from a connection
func NewStreamReader(conn net.Conn) *StreamReader {
	return &StreamReader{conn: conn}
}

// Read reads the next streaming message
// Returns io.EOF when stream is closed normally
func (s *StreamReader) Read() (*Response, byte, error) {
	var resp Response
	msgType, err := ReadJSONFrame(s.conn, &resp)
	if err != nil {
		return nil, 0, err
	}
	return &resp, msgType, nil
}

// ReadBinary reads the next binary frame
func (s *StreamReader) ReadBinary() ([]byte, error) {
	frame, err := ReadFrame(s.conn)
	if err != nil {
		return nil, err
	}
	if frame.Type != MsgTypeBinary {
		return nil, fmt.Errorf("expected binary frame, got type 0x%02x", frame.Type)
	}
	return frame.Payload, nil
}

// Close closes the underlying connection
func (s *StreamReader) Close() error {
	return s.conn.Close()
}

// StreamWriter provides streaming message writing to a persistent connection
type StreamWriter struct {
	conn net.Conn
}

// NewStreamWriter creates a streaming writer from a connection
func NewStreamWriter(conn net.Conn) *StreamWriter {
	return &StreamWriter{conn: conn}
}

// WriteStream writes a streaming response message
func (s *StreamWriter) WriteStream(resp *Response) error {
	return WriteStreamFrame(s.conn, resp)
}

// WriteResponse writes a final response message
func (s *StreamWriter) WriteResponse(resp *Response) error {
	return WriteResponseFrame(s.conn, resp)
}

// WriteBinary writes a binary chunk
func (s *StreamWriter) WriteBinary(data []byte) error {
	return WriteBinaryFrame(s.conn, data)
}

// Close closes the underlying connection
func (s *StreamWriter) Close() error {
	return s.conn.Close()
}

// BinaryReader wraps a StreamReader to read binary chunks as an io.Reader
type BinaryReader struct {
	stream *StreamReader
	buf    []byte
	pos    int
}

// NewBinaryReader creates a reader that streams binary chunks
func NewBinaryReader(stream *StreamReader) *BinaryReader {
	return &BinaryReader{stream: stream}
}

// Read implements io.Reader by reading binary frames
func (b *BinaryReader) Read(p []byte) (n int, err error) {
	// If we have buffered data, return it first
	if b.pos < len(b.buf) {
		n = copy(p, b.buf[b.pos:])
		b.pos += n
		return n, nil
	}

	// Read next binary frame
	chunk, err := b.stream.ReadBinary()
	if err != nil {
		return 0, err
	}

	// If chunk is empty, we're done
	if len(chunk) == 0 {
		return 0, io.EOF
	}

	// Copy to output buffer
	n = copy(p, chunk)

	// If chunk is larger than output buffer, save remainder
	if n < len(chunk) {
		b.buf = chunk
		b.pos = n
	}

	return n, nil
}

// Close closes the underlying stream
func (b *BinaryReader) Close() error {
	return b.stream.Close()
}
