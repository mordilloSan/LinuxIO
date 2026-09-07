package app

import (
	"strings"
	"testing"
)

func TestARCSizeFromReaderParsesArcstatsSizeField(t *testing.T) {
	const arcstats = `
13 1 0x01 95 4560 123456789 987654321
name                            type data
hits                            4    1234
size                            4    987654321
compressed_size                 4    111
`

	got, err := arcSizeFromReader(strings.NewReader(arcstats))
	if err != nil {
		t.Fatal(err)
	}
	if got != 987654321 {
		t.Fatalf("arcSizeFromReader() = %d, want %d", got, uint64(987654321))
	}
}

func TestARCSizeFromReaderRequiresExactSizeField(t *testing.T) {
	_, err := arcSizeFromReader(strings.NewReader("size_bytes 4 123\n"))
	if err == nil {
		t.Fatal("arcSizeFromReader() succeeded for size_bytes, want missing size error")
	}
}

func TestARCSizeFromReaderRejectsMalformedSizeLine(t *testing.T) {
	_, err := arcSizeFromReader(strings.NewReader("size 4\n"))
	if err == nil {
		t.Fatal("arcSizeFromReader() succeeded for malformed size line")
	}
}

func TestARCSizeFromReaderRejectsInvalidSizeValue(t *testing.T) {
	_, err := arcSizeFromReader(strings.NewReader("size 4 not-a-number\n"))
	if err == nil {
		t.Fatal("arcSizeFromReader() succeeded for invalid size value")
	}
}
