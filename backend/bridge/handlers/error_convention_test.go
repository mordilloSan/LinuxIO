package handlers

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// Client errors are classified by wrapping a bridgeipc sentinel with %w, never
// by encoding the status in the error text.
var statusInErrorText = [][]byte{
	[]byte(`"bad_request:`),
	[]byte(`"%w: invalid arguments`),
	[]byte(`"%w: not found`),
	[]byte(`"%w: forbidden`),
	[]byte(`"%w: conflict`),
}

func TestHandlersDoNotEncodeStatusInErrorText(t *testing.T) {
	if err := walkGoFiles(func(path string) error {
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range bytes.Split(src, []byte("\n")) {
			for _, pattern := range statusInErrorText {
				if bytes.Contains(line, pattern) {
					t.Errorf("%s:%d: status encoded in error text %s; wrap a bridgeipc sentinel with %%w instead", path, i+1, strings.TrimPrefix(string(pattern), `"`))
					break
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
