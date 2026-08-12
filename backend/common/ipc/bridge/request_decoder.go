package bridge

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
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
		if err := jsonv2.Unmarshal(raw, &request, jsonv2.RejectUnknownMembers(true)); err != nil {
			return nil, err
		}
		return request, nil
	}
}
