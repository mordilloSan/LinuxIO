package httpapi

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrepareUnixSocket(t *testing.T) {
	t.Run("creates parent directory", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "nested", "agent.sock")
		require.NoError(t, prepareUnixSocket(path))
		assert.DirExists(t, filepath.Dir(path))
	})

	t.Run("rejects non-socket file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "agent.sock")
		require.NoError(t, os.WriteFile(path, []byte("data"), 0o600))
		err := prepareUnixSocket(path)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not a socket")
	})

	t.Run("removes stale socket", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "agent.sock")
		listener, err := net.Listen("unix", path)
		require.NoError(t, err)
		// Keep the socket file on close so it is stale, as after a crash.
		unixListener, ok := listener.(*net.UnixListener)
		require.True(t, ok)
		unixListener.SetUnlinkOnClose(false)
		require.NoError(t, listener.Close())
		require.FileExists(t, path)

		require.NoError(t, prepareUnixSocket(path))
		assert.NoFileExists(t, path)
	})

	t.Run("refuses live socket", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "agent.sock")
		listener, err := net.Listen("unix", path)
		require.NoError(t, err)
		defer listener.Close()
		go func() {
			for {
				conn, acceptErr := listener.Accept()
				if acceptErr != nil {
					return
				}
				_ = conn.Close()
			}
		}()

		err = prepareUnixSocket(path)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already in use")
	})
}

func TestOpenListenerUnixSocketPermissions(t *testing.T) {
	tests := []struct {
		name     string
		mode     os.FileMode
		expected os.FileMode
	}{
		{name: "default", mode: 0, expected: 0o660},
		{name: "world readable", mode: 0o666, expected: 0o666},
		{name: "root only", mode: 0o600, expected: 0o600},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "agent.sock")
			listener, err := OpenListener("unix:"+path, tt.mode)
			require.NoError(t, err)
			defer listener.Close()

			info, err := os.Stat(path)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, info.Mode().Perm())
		})
	}
}
