package bridgeipc

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
)

// JSONRequestDecoder returns the shared strict decoder for bridge route
// requests. Missing and null requests retain the existing empty-object
// behavior; required-field semantics remain the handler's responsibility.
func JSONRequestDecoder[T any]() RequestDecoder {
	return func(raw jsontext.Value) (any, error) {
		if len(raw) == 0 || string(raw) == "null" {
			raw = jsontext.Value("{}")
		}

		var request T
		if err := json.Unmarshal(raw, &request, json.RejectUnknownMembers(true)); err != nil {
			return nil, err
		}
		return request, nil
	}
}
