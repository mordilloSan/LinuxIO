package docker

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/userconfig"
	"github.com/mordilloSan/LinuxIO/backend/common/hostcmd"
)

// stubHostIPWaitTools resolves the drop-in's tools to fixed paths, or fails
// the lookup of the one named missing.
func stubHostIPWaitTools(t *testing.T, missing string) {
	t.Helper()
	previous := hostIPWaitLookPath
	t.Cleanup(func() { hostIPWaitLookPath = previous })
	paths := map[string]string{"timeout": "/usr/bin/timeout", "sh": "/bin/sh", "ip": "/usr/sbin/ip"}
	hostIPWaitLookPath = func(name string) (string, error) {
		if name == missing {
			return "", exec.ErrNotFound
		}
		return paths[name], nil
	}
}

// countDaemonReloads replaces systemd's daemon-reload with a counter.
func countDaemonReloads(t *testing.T) *int {
	t.Helper()
	previous := systemdDaemonReload
	t.Cleanup(func() { systemdDaemonReload = previous })
	count := 0
	systemdDaemonReload = func(context.Context) error {
		count++
		return nil
	}
	return &count
}

const hostIPWaitHeader = "# Managed by LinuxIO. Do not edit by hand.\n[Service]\n"

func hostIPWaitLine(ip string) string {
	return "ExecStartPre=-/usr/bin/timeout 30 /bin/sh -c 'until /usr/sbin/ip -o addr show to " + ip + " -tentative | grep -q .; do sleep 0.5; done'\n"
}

func TestRenderDockerHostIPWaitDropIn(t *testing.T) {
	stubHostIPWaitTools(t, "")
	tests := []struct {
		name string
		ips  []string
		want string
	}{
		{"one IPv4", []string{"192.168.1.66"}, hostIPWaitHeader + hostIPWaitLine("192.168.1.66")},
		{"one IPv6", []string{"2001:db8::66"}, hostIPWaitHeader + hostIPWaitLine("2001:db8::66")},
		{
			"several, in the stored order", []string{"192.168.1.66", "2001:db8::66", "10.0.0.2"},
			hostIPWaitHeader + hostIPWaitLine("192.168.1.66") + hostIPWaitLine("2001:db8::66") + hostIPWaitLine("10.0.0.2"),
		},
		{"written in the canonical form", []string{" 2001:DB8:0::66 ", "::ffff:10.0.0.2"}, hostIPWaitHeader + hostIPWaitLine("2001:db8::66") + hostIPWaitLine("10.0.0.2")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content, err := renderDockerHostIPWaitDropIn(tt.ips)
			require.NoError(t, err)
			require.Equal(t, tt.want, content)
		})
	}
}

// The drop-in names addresses only, so a bridge handoff that moves an address
// to another interface leaves it valid.
func TestRenderDockerHostIPWaitDropInNamesNoInterface(t *testing.T) {
	stubHostIPWaitTools(t, "")
	content, err := renderDockerHostIPWaitDropIn([]string{"192.168.1.66", "2001:db8::66"})
	require.NoError(t, err)
	for _, word := range []string{" dev ", "enp", "eth", "br-", "%", "$", "network-online", "wait-online"} {
		require.NotContains(t, content, word)
	}
}

func TestRenderDockerHostIPWaitDropInRejectsInvalidInput(t *testing.T) {
	stubHostIPWaitTools(t, "")
	for name, ips := range map[string][]string{
		"empty list":    nil,
		"not an IP":     {"192.168.1.66; rm -rf /"},
		"loopback":      {"127.0.0.1"},
		"zoned address": {"fe80::1%eth0"},
	} {
		_, err := renderDockerHostIPWaitDropIn(ips)
		require.Error(t, err, name)
	}
}

func TestRenderDockerHostIPWaitDropInNamesTheMissingTool(t *testing.T) {
	for _, tool := range []string{"timeout", "sh", "ip"} {
		stubHostIPWaitTools(t, tool)
		_, err := renderDockerHostIPWaitDropIn([]string{"192.168.1.66"})
		require.ErrorIs(t, err, exec.ErrNotFound, tool)
		require.ErrorContains(t, err, "find "+tool+" ", tool)
	}
}

func TestRenderDockerHostIPWaitDropInRefusesAToolPathWithSyntax(t *testing.T) {
	for _, path := range []string{"timeout", "./timeout", "/opt/my tools/timeout", "/opt/$HOME/timeout", "/opt/%n/timeout", "/opt/'/timeout", "/opt/a;b/timeout"} {
		previous := hostIPWaitLookPath
		hostIPWaitLookPath = func(string) (string, error) { return path, nil }
		_, err := renderDockerHostIPWaitDropIn([]string{"192.168.1.66"})
		hostIPWaitLookPath = previous
		require.ErrorContains(t, err, "cannot go into a systemd command line", path)
	}
}

func TestSyncDaemonHostIPWaitWritesAndRemovesTheDropIn(t *testing.T) {
	stubHostIPWaitTools(t, "")
	reloads := countDaemonReloads(t)
	path := filepath.Join(t.TempDir(), "docker.service.d", "linuxio-wait-host-ips.conf")
	on := userconfig.Docker{WaitForHostIPs: true, HostIPs: []string{"192.168.1.66"}}

	require.NoError(t, syncDaemonHostIPWait(context.Background(), path, on))
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, hostIPWaitHeader+hostIPWaitLine("192.168.1.66"), string(content))
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o644), info.Mode().Perm())
	require.Equal(t, 1, *reloads)

	// Off removes the file and keeps the stored list out of it.
	off := userconfig.Docker{HostIPs: []string{"192.168.1.66"}}
	require.NoError(t, syncDaemonHostIPWait(context.Background(), path, off))
	_, err = os.Stat(path)
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Equal(t, 2, *reloads)

	// Off with no file is a no-op: nothing to reload.
	require.NoError(t, syncDaemonHostIPWait(context.Background(), path, off))
	require.Equal(t, 2, *reloads)
}

func TestSyncDaemonHostIPWaitWritesNothingItCannotRender(t *testing.T) {
	reloads := countDaemonReloads(t)
	path := filepath.Join(t.TempDir(), "docker.service.d", "linuxio-wait-host-ips.conf")

	stubHostIPWaitTools(t, "")
	err := syncDaemonHostIPWait(context.Background(), path, userconfig.Docker{WaitForHostIPs: true})
	require.ErrorContains(t, err, "docker host IPs cannot be empty")

	stubHostIPWaitTools(t, "ip")
	err = syncDaemonHostIPWait(context.Background(), path, userconfig.Docker{WaitForHostIPs: true, HostIPs: []string{"192.168.1.66"}})
	require.ErrorIs(t, err, exec.ErrNotFound)

	_, err = os.Stat(filepath.Dir(path))
	require.ErrorIs(t, err, os.ErrNotExist)
	require.Zero(t, *reloads)
}

func publishing(ips ...string) container.InspectResponse {
	bindings := make([]network.PortBinding, 0, len(ips))
	for _, ip := range ips {
		binding := network.PortBinding{HostPort: "53"}
		if ip != "" {
			binding.HostIP = netip.MustParseAddr(ip)
		}
		bindings = append(bindings, binding)
	}
	return container.InspectResponse{HostConfig: &container.HostConfig{
		PortBindings: network.PortMap{network.MustParsePort("53/udp"): bindings},
	}}
}

func TestPublishedHostIPsKeepsSpecificAddressesOnly(t *testing.T) {
	got := publishedHostIPs([]container.InspectResponse{
		publishing("192.168.1.66", "0.0.0.0", "", "::", "127.0.0.1", "::1"),
		publishing("2001:db8::66", "::ffff:192.168.1.66", "::ffff:127.0.0.1", "fe80::1%eth0"),
		publishing("10.0.0.2", "192.168.1.66"),
		{}, // no HostConfig
	})
	require.Equal(t, []string{"10.0.0.2", "192.168.1.66", "2001:db8::66"}, got)

	empty := publishedHostIPs(nil)
	require.NotNil(t, empty)
	require.Empty(t, empty)
}

type fakeHostIPClient struct {
	items    []container.Summary
	inspects map[string]container.InspectResponse
	listErr  error
}

func (f fakeHostIPClient) ContainerList(_ context.Context, options client.ContainerListOptions) (client.ContainerListResult, error) {
	if !options.All {
		return client.ContainerListResult{}, errors.New("stopped containers were left out")
	}
	return client.ContainerListResult{Items: f.items}, f.listErr
}

func (f fakeHostIPClient) ContainerInspect(_ context.Context, id string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	inspect, ok := f.inspects[id]
	if !ok {
		return client.ContainerInspectResult{}, errdefs.ErrNotFound
	}
	return client.ContainerInspectResult{Container: inspect}, nil
}

// A container that failed to start has no ports in the list, so every
// container is inspected, stopped ones included; one removed meanwhile is
// skipped.
func TestInspectContainersReadsEveryContainer(t *testing.T) {
	cli := fakeHostIPClient{
		items: []container.Summary{{ID: "running"}, {ID: "failed", State: container.StateExited}, {ID: "removed"}},
		inspects: map[string]container.InspectResponse{
			"running": publishing("10.0.0.2"),
			"failed":  publishing("192.168.1.66"),
		},
	}
	inspects, err := inspectContainers(context.Background(), cli)
	require.NoError(t, err)
	require.Equal(t, []string{"10.0.0.2", "192.168.1.66"}, publishedHostIPs(inspects))

	cli.listErr = errors.New("daemon down")
	_, err = inspectContainers(context.Background(), cli)
	require.ErrorContains(t, err, "daemon down")
}

func ipNet(cidr string) *net.IPNet {
	ip, prefix, err := net.ParseCIDR(cidr)
	if err != nil {
		panic(err)
	}
	prefix.IP = ip
	return prefix
}

// A link-local address the host holds stays in the list: one that is listed
// or published must not be marked as missing.
func TestAssignedHostIPsDropsLoopbackOnly(t *testing.T) {
	got := assignedHostIPs([]net.Addr{
		ipNet("127.0.0.1/8"), ipNet("::1/128"),
		ipNet("fe80::1/64"), ipNet("169.254.10.1/16"),
		ipNet("2001:db8::66/64"), ipNet("192.168.1.66/24"),
		&net.IPNet{IP: net.ParseIP("10.0.0.2"), Mask: net.CIDRMask(8, 32)}, // 16-byte, IPv4-mapped
		ipNet("192.168.1.66/24"),
		&net.UnixAddr{Name: "/run/x", Net: "unix"},
	})
	require.Equal(t, []string{"10.0.0.2", "169.254.10.1", "192.168.1.66", "2001:db8::66", "fe80::1"}, got)

	empty := assignedHostIPs(nil)
	require.NotNil(t, empty)
	require.Empty(t, empty)
}

// The assigned list is built from addresses alone, so it reads the same with
// the address on a network card and on the bridge a handoff moved it to, and
// compares as plain strings with what the config stores.
func TestAssignedHostIPsMatchTheStoredFormOnAnyInterface(t *testing.T) {
	onCard := assignedHostIPs([]net.Addr{ipNet("192.168.1.66/24"), ipNet("2001:DB8:0::66/64")})
	onBridge := assignedHostIPs([]net.Addr{ipNet("2001:db8::66/64"), ipNet("192.168.1.66/24")})
	require.Equal(t, onCard, onBridge)

	stored, err := userconfig.ParseDockerHostIPs([]string{"192.168.1.66", "2001:DB8:0::66"})
	require.NoError(t, err)
	require.ElementsMatch(t, stored, onBridge)
}

func bridgeNetwork(driver string, gateways ...string) network.Summary {
	var summary network.Summary
	summary.Driver = driver
	for _, gateway := range gateways {
		config := network.IPAMConfig{}
		if gateway != "" {
			config.Gateway = netip.MustParseAddr(gateway)
		}
		summary.IPAM.Config = append(summary.IPAM.Config, config)
	}
	return summary
}

// Docker assigns the gateway of a bridge network itself, after it starts, so
// waiting for one before Docker starts takes the whole limit at every boot.
func TestDockerBridgeGatewaysAreNeitherSuggestedNorAssigned(t *testing.T) {
	gateways := dockerBridgeGateways([]network.Summary{
		bridgeNetwork("bridge", "172.17.0.1"),
		bridgeNetwork("bridge", "172.18.0.1", "fd00:18::1", ""),
		bridgeNetwork("macvlan", "192.168.1.1"), // the LAN router, not a host address
		bridgeNetwork("host"),
	})
	require.Equal(t, []string{"172.17.0.1", "172.18.0.1", "fd00:18::1"}, gateways)

	published := publishedHostIPs([]container.InspectResponse{publishing("172.17.0.1", "192.168.1.66")})
	require.Equal(t, []string{"192.168.1.66"}, withoutHostIPs(published, gateways))
	assigned := assignedHostIPs([]net.Addr{ipNet("172.17.0.1/16"), ipNet("192.168.1.66/24"), ipNet("fd00:18::1/64")})
	require.Equal(t, []string{"192.168.1.66"}, withoutHostIPs(assigned, gateways))

	empty := withoutHostIPs(nil, gateways)
	require.NotNil(t, empty)
	require.Empty(t, empty)
	require.Empty(t, dockerBridgeGateways(nil))
}

// The path goes into a file root runs from at boot, so it comes from the
// system folders and never from PATH, which starts with /usr/local.
func TestLookSystemPathIgnoresPATH(t *testing.T) {
	onPath, system := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(onPath, "ip"), []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(onPath, "timeout"), []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(system, "ip"), []byte("#!/bin/sh\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(system, "sh"), []byte("not executable"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(system, "grep"), 0o755))
	t.Setenv("PATH", onPath)
	previous := hostcmd.SearchDirs
	t.Cleanup(func() { hostcmd.SearchDirs = previous })
	hostcmd.SearchDirs = []string{filepath.Join(system, "missing"), system}

	path, err := lookSystemPath("ip")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(system, "ip"), path)
	for _, name := range []string{"timeout", "sh", "grep"} {
		_, err = lookSystemPath(name)
		require.ErrorIs(t, err, exec.ErrNotFound, name)
	}
}

func TestCheckDaemonHostIPWaitRendersWithoutWriting(t *testing.T) {
	reloads := countDaemonReloads(t)
	on := userconfig.Docker{WaitForHostIPs: true, HostIPs: []string{"192.168.1.66"}}

	stubHostIPWaitTools(t, "")
	require.NoError(t, CheckDaemonHostIPWait(on))
	require.NoError(t, CheckDaemonHostIPWait(userconfig.Docker{HostIPs: []string{"192.168.1.66"}}))

	stubHostIPWaitTools(t, "timeout")
	require.ErrorIs(t, CheckDaemonHostIPWait(on), exec.ErrNotFound)
	// Off needs no tool: switching off must work on a host that lost one.
	require.NoError(t, CheckDaemonHostIPWait(userconfig.Docker{}))
	require.Zero(t, *reloads)
}
