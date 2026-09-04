package app

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetAddressSpecialForms(t *testing.T) {
	tests := []struct {
		name     string
		listen   string
		expected string
	}{
		{name: "unix prefix", listen: "unix:/run/go-monitoring/agent.sock", expected: "unix:/run/go-monitoring/agent.sock"},
		{name: "bare absolute path", listen: "/run/go-monitoring/agent.sock", expected: "/run/go-monitoring/agent.sock"},
		{name: "disabled none", listen: "none", expected: ListenDisabled},
		{name: "disabled off", listen: "OFF", expected: ListenDisabled},
		{name: "disabled with spaces", listen: " disabled ", expected: ListenDisabled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, GetAddress(tt.listen))
		})
	}
}

func TestIsListenDisabled(t *testing.T) {
	assert.True(t, IsListenDisabled("none"))
	assert.True(t, IsListenDisabled("Off"))
	assert.True(t, IsListenDisabled("DISABLED"))
	assert.False(t, IsListenDisabled(""))
	assert.False(t, IsListenDisabled("127.0.0.1:45876"))
	assert.False(t, IsListenDisabled("unix:/tmp/agent.sock"))
}

func TestSplitListenAddress(t *testing.T) {
	tests := []struct {
		addr    string
		network string
		address string
	}{
		{addr: "127.0.0.1:45876", network: "tcp", address: "127.0.0.1:45876"},
		{addr: ":9000", network: "tcp", address: ":9000"},
		{addr: "unix:/run/agent.sock", network: "unix", address: "/run/agent.sock"},
		{addr: "unix:relative.sock", network: "unix", address: "relative.sock"},
		{addr: "/run/agent.sock", network: "unix", address: "/run/agent.sock"},
	}
	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			network, address := SplitListenAddress(tt.addr)
			assert.Equal(t, tt.network, network)
			assert.Equal(t, tt.address, address)
		})
	}
}

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
			listener, err := openListener("unix:"+path, tt.mode)
			require.NoError(t, err)
			defer listener.Close()

			info, err := os.Stat(path)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, info.Mode().Perm())
		})
	}
}
