package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
