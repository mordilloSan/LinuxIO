package ipc

import (
	"encoding/json"
	"fmt"
	"net"
)

// RequestContext wraps optional streaming helpers available to
// bridge handlers processing framed requests.
type RequestContext struct {
	conn   net.Conn
	stream *StreamWriter
}

// NewRequestContext builds a context tied to the provided connection.
func NewRequestContext(conn net.Conn) *RequestContext {
	if conn == nil {
		return &RequestContext{}
	}
	return &RequestContext{
		conn:   conn,
		stream: NewStreamWriter(conn),
	}
}

// Conn exposes the underlying net.Conn so handlers can use low-level
// framing helpers like ReadFrame / WriteResponseFrame directly.
func (rc *RequestContext) Conn() net.Conn {
	if rc == nil {
		return nil
	}
	return rc.conn
}

// HasStream reports whether streaming helpers are enabled.
func (rc *RequestContext) HasStream() bool {
	return rc != nil && rc.stream != nil
}

// SendStream encodes the response and writes it as a streaming frame.
func (rc *RequestContext) SendStream(resp *Response) error {
	if rc == nil || rc.stream == nil {
		return fmt.Errorf("streaming not available")
	}
	return rc.stream.WriteStream(resp)
}

// SendStreamJSON marshals payload and sends it with the provided status.
func (rc *RequestContext) SendStreamJSON(status string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return rc.SendStream(&Response{Status: status, Output: raw})
}
