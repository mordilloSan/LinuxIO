package apischema

// Optional returns a pointer to value for an optional DTO field, or nil when
// value is the zero value so the field is omitted from the wire payload.
func Optional[T comparable](value T) *T {
	var zero T
	if value == zero {
		return nil
	}
	return &value
}

// StringValue dereferences an optional string, treating nil as "".
func StringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
