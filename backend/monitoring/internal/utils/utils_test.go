package utils

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTwoDecimals(t *testing.T) {
	tests := []struct {
		name     string
		input    float64
		expected float64
	}{
		{"round down", 1.234, 1.23},
		{"round half up", 1.235, 1.24}, // math.Round rounds half up
		{"no rounding needed", 1.23, 1.23},
		{"negative number", -1.235, -1.24}, // math.Round rounds half up (more negative)
		{"zero", 0.0, 0.0},
		{"large number", 123.456, 123.46}, // rounds 5 up
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TwoDecimals(tt.input)
			assert.InDelta(t, tt.expected, result, 0.0001)
		})
	}
}

func TestBytesToMegabytes(t *testing.T) {
	tests := []struct {
		name     string
		input    float64
		expected float64
	}{
		{"1 MB", 1048576, 1.0},
		{"512 KB", 524288, 0.5},
		{"zero", 0, 0},
		{"large value", 1073741824, 1024}, // 1 GB = 1024 MB
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BytesToMegabytes(tt.input)
			assert.InDelta(t, tt.expected, result, 0.0001)
		})
	}
}

func TestBytesToGigabytes(t *testing.T) {
	tests := []struct {
		name     string
		input    uint64
		expected float64
	}{
		{"1 GB", 1073741824, 1.0},
		{"512 MB", 536870912, 0.5},
		{"0 GB", 0, 0},
		{"2 GB", 2147483648, 2.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BytesToGigabytes(tt.input)
			assert.InDelta(t, tt.expected, result, 0.0001)
		})
	}
}

func TestFileFunctions(t *testing.T) {
	tmpDir := t.TempDir()
	testFilePath := filepath.Join(tmpDir, "test.txt")
	testContent := "hello world"

	// Test FileExists (false)
	assert.False(t, FileExists(testFilePath))

	// Test ReadStringFileOK (false)
	content, ok := ReadStringFileOK(testFilePath)
	assert.False(t, ok)
	assert.Empty(t, content)

	// Test ReadStringFile (empty)
	assert.Empty(t, ReadStringFile(testFilePath))

	// Write file
	err := os.WriteFile(testFilePath, []byte(testContent+"\n "), 0644)
	require.NoError(t, err)

	// Test FileExists (true)
	assert.True(t, FileExists(testFilePath))

	// Test ReadStringFileOK (true)
	content, ok = ReadStringFileOK(testFilePath)
	assert.True(t, ok)
	assert.Equal(t, testContent, content)

	// Test ReadStringFile (content)
	assert.Equal(t, testContent, ReadStringFile(testFilePath))
}

func TestReadUintFile(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("valid uint", func(t *testing.T) {
		path := filepath.Join(tmpDir, "uint.txt")
		require.NoError(t, os.WriteFile(path, []byte(" 12345\n"), 0644))
		val, ok := ReadUintFile(path)
		assert.True(t, ok)
		assert.Equal(t, uint64(12345), val)
	})

	t.Run("invalid uint", func(t *testing.T) {
		path := filepath.Join(tmpDir, "invalid.txt")
		require.NoError(t, os.WriteFile(path, []byte("abc"), 0644))
		val, ok := ReadUintFile(path)
		assert.False(t, ok)
		assert.Equal(t, uint64(0), val)
	})

	t.Run("missing file", func(t *testing.T) {
		path := filepath.Join(tmpDir, "missing.txt")
		val, ok := ReadUintFile(path)
		assert.False(t, ok)
		assert.Equal(t, uint64(0), val)
	})
}

func TestReadStringFileLimited(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "limited.txt")
	require.NoError(t, os.WriteFile(path, []byte("abcdef\n"), 0644))

	t.Run("reads and trims within limit", func(t *testing.T) {
		got, err := ReadStringFileLimited(path, 16)
		require.NoError(t, err)
		assert.Equal(t, "abcdef", got)
	})

	t.Run("caps read length", func(t *testing.T) {
		got, err := ReadStringFileLimited(path, 3)
		require.NoError(t, err)
		assert.Equal(t, "abc", got)
	})

	t.Run("missing file returns read error", func(t *testing.T) {
		got, err := ReadStringFileLimited(filepath.Join(tmpDir, "missing.txt"), 16)
		require.Error(t, err)
		assert.Empty(t, got)
	})
}

func TestGetEnv(t *testing.T) {
	key := "TEST_VAR"

	t.Run("variable exists", func(t *testing.T) {
		t.Setenv(key, "some_val")

		val, exists := GetEnv(key)
		assert.True(t, exists)
		assert.Equal(t, "some_val", val)
	})

	t.Run("variable does not exist", func(t *testing.T) {
		val, exists := GetEnv(key)
		assert.False(t, exists)
		assert.Empty(t, val)
	})
}
