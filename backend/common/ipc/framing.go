package ipc

import (
	"encoding/json"
	"io"
)

// WriteRequest writes a Request as JSON to the stream
func WriteRequest(w io.Writer, req *Request) error {
	return json.NewEncoder(w).Encode(req)
}

// ReadRequest reads a Request as JSON from the stream
func ReadRequest(r io.Reader) (*Request, error) {
	var req Request
	if err := json.NewDecoder(r).Decode(&req); err != nil {
		return nil, err
	}
	return &req, nil
}

// WriteResponse writes a Response as JSON to the stream
func WriteResponse(w io.Writer, resp *Response) error {
	return json.NewEncoder(w).Encode(resp)
}

// ReadResponse reads a Response as JSON from the stream
func ReadResponse(r io.Reader) (*Response, error) {
	var resp Response
	if err := json.NewDecoder(r).Decode(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
