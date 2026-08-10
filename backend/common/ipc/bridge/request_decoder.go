package bridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// JSONRequestDecoder returns the shared strict decoder for bridge route
// requests. Missing and null requests retain the existing empty-object
// behavior; required-field semantics remain the handler's responsibility.
func JSONRequestDecoder[T any]() RequestDecoder {
	return func(raw json.RawMessage) (any, error) {
		if len(raw) == 0 || string(raw) == "null" {
			raw = json.RawMessage("{}")
		}

		var request T
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			return nil, err
		}

		var trailing any
		if err := decoder.Decode(&trailing); err != io.EOF {
			if err == nil {
				return nil, errors.New("request must contain exactly one JSON value")
			}
			return nil, err
		}
		return request, nil
	}
}
