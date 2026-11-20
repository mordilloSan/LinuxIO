package docker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dockerclient"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/userconfig"
	"github.com/mordilloSan/LinuxIO/backend/common/atomicfile"
	"github.com/mordilloSan/LinuxIO/backend/common/hostcmd"
)

const (
	dockerHostIPWaitDropInPath = "/etc/systemd/system/docker.service.d/linuxio-wait-host-ips.conf"
	// dockerHostIPWaitSeconds is how long docker.service waits for one address
	// before it starts anyway. The settings page states the same number.
	dockerHostIPWaitSeconds = 30
)

var (
	// hostIPWaitLookPath finds the tools the drop-in names; tests replace it.
	hostIPWaitLookPath = lookSystemPath
	// hostIPWaitToolPath is what a tool path may hold to go unquoted into a
	// systemd command line: nothing systemd or the shell reads as syntax.
	hostIPWaitToolPath = regexp.MustCompile(`^(/[A-Za-z0-9._+-]+)+$`)
)

// SyncDaemonHostIPWait writes (or removes, when the setting is off) the
// systemd drop-in that makes docker.service wait until the configured host
// addresses are assigned. The config handler calls it when the setting or its
// list changes.
func SyncDaemonHostIPWait(ctx context.Context, docker userconfig.Docker) error {
	return syncDaemonHostIPWait(ctx, dockerHostIPWaitDropInPath, docker)
}

func syncDaemonHostIPWait(ctx context.Context, path string, docker userconfig.Docker) error {
	if !docker.WaitForHostIPs {
		return removeDockerMountOrderingDropIn(ctx, path)
	}

	content, err := renderDockerHostIPWaitDropIn(docker.HostIPs)
	if err != nil {
		return err
	}
	if err := ensureSystemdDropInDir(filepath.Dir(path)); err != nil {
		return err
	}
	if err := atomicfile.Write(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write docker service drop-in: %w", err)
	}
	return systemdDaemonReload(ctx)
}

// renderDockerHostIPWaitDropIn returns one ExecStartPre line per address.
//
// The line waits for the address, not for a network card or for
// network-online.target, so it holds on networkd and NetworkManager hosts and
// after a bridge handoff moved the address to another interface. The time
// limit is inside the command because a timeout of ExecStartPre itself fails
// the unit even with the "-" prefix, and Docker must start when an address
// never appears. "-tentative" leaves out an IPv6 address still in duplicate
// address detection, which cannot be bound yet.
//
// The addresses are parsed again here, so the only request value in the line
// is the text of a parsed address.
func renderDockerHostIPWaitDropIn(values []string) (string, error) {
	ips, err := userconfig.ParseDockerHostIPs(values)
	if err != nil {
		return "", err
	}
	if len(ips) == 0 {
		return "", errors.New("docker host IPs cannot be empty")
	}
	timeoutPath, err := hostIPWaitTool("timeout")
	if err != nil {
		return "", err
	}
	shPath, err := hostIPWaitTool("sh")
	if err != nil {
		return "", err
	}
	ipPath, err := hostIPWaitTool("ip")
	if err != nil {
		return "", err
	}

	var b strings.Builder
	b.WriteString("# Managed by LinuxIO. Do not edit by hand.\n")
	b.WriteString("[Service]\n")
	for _, ip := range ips {
		fmt.Fprintf(&b, "ExecStartPre=-%s %d %s -c 'until %s -o addr show to %s -tentative | grep -q .; do sleep 0.5; done'\n",
			timeoutPath, dockerHostIPWaitSeconds, shPath, ipPath, ip)
	}
	return b.String(), nil
}

// lookSystemPath finds name in the system folders only and never through
// PATH. The path is written into a file root runs from at every boot, and PATH
// starts with /usr/local, which a group other than root can write on some
// distributions.
func lookSystemPath(name string) (string, error) {
	for _, dir := range hostcmd.SearchDirs {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
			return path, nil
		}
	}
	return "", exec.ErrNotFound
}

// CheckDaemonHostIPWait reports whether the drop-in for docker can be
// rendered on this host, so the config handler can refuse to store a setting
// whose file it could not write. It writes nothing.
func CheckDaemonHostIPWait(docker userconfig.Docker) error {
	if !docker.WaitForHostIPs {
		return nil
	}
	_, err := renderDockerHostIPWaitDropIn(docker.HostIPs)
	return err
}

func hostIPWaitTool(name string) (string, error) {
	path, err := hostIPWaitLookPath(name)
	if err != nil {
		return "", fmt.Errorf("find %s for the docker host IP wait: %w", name, err)
	}
	if !hostIPWaitToolPath.MatchString(path) {
		return "", fmt.Errorf("%s is at %q, which cannot go into a systemd command line", name, path)
	}
	return path, nil
}

type hostIPContainerClient interface {
	ContainerList(context.Context, client.ContainerListOptions) (client.ContainerListResult, error)
	containerInspector
}

// ListHostIPs returns the host addresses containers publish ports on and the
// addresses the host holds now. With Docker unreachable the published list is
// empty and the assigned one is still returned, since the setting can be
// edited while Docker is down.
//
// The gateway of a Docker bridge network (172.17.0.1 on docker0) is in
// neither list. Docker assigns it itself after it starts, so waiting for it
// before Docker starts always takes the whole time limit: it is never
// suggested, and one typed into the list is marked as not assigned.
func ListHostIPs(ctx context.Context) (apischema.DockerHostIPs, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return apischema.DockerHostIPs{}, fmt.Errorf("read host interface addresses: %w", err)
	}
	result := apischema.DockerHostIPs{Published: []string{}, Assigned: assignedHostIPs(addrs)}

	inspects, networks, err := readDockerHostIPState(ctx)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return apischema.DockerHostIPs{}, ctxErr
		}
		slog.Debug("published host IPs unavailable", "component", "docker", "error", err)
		return result, nil
	}
	gateways := dockerBridgeGateways(networks)
	result.Published = withoutHostIPs(publishedHostIPs(inspects), gateways)
	result.Assigned = withoutHostIPs(result.Assigned, gateways)
	return result, nil
}

func readDockerHostIPState(ctx context.Context) ([]container.InspectResponse, []network.Summary, error) {
	cli, err := dockerclient.Get()
	if err != nil {
		return nil, nil, fmt.Errorf("docker client error: %w", err)
	}
	defer dockerclient.Release(cli)

	inspects, err := inspectContainers(ctx, cli)
	if err != nil {
		return nil, nil, err
	}
	networks, err := cli.NetworkList(ctx, client.NetworkListOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list networks: %w", err)
	}
	return inspects, networks.Items, nil
}

// dockerBridgeGateways returns the gateway addresses of Docker's bridge
// networks, which Docker puts on the bridges it creates on the host. The
// gateway of a macvlan or ipvlan network is a router on the LAN and is left
// out.
func dockerBridgeGateways(networks []network.Summary) []string {
	var gateways []netip.Addr
	for _, item := range networks {
		if item.Driver != "bridge" {
			continue
		}
		for _, config := range item.IPAM.Config {
			if addr, ok := waitableHostIP(config.Gateway); ok {
				gateways = append(gateways, addr)
			}
		}
	}
	return hostIPStrings(gateways)
}

// withoutHostIPs returns ips without the addresses in drop, never nil.
func withoutHostIPs(ips, drop []string) []string {
	kept := make([]string, 0, len(ips))
	for _, ip := range ips {
		if !slices.Contains(drop, ip) {
			kept = append(kept, ip)
		}
	}
	return kept
}

// inspectContainers inspects every container, running or not: the container
// list reports no ports for one that failed to start, which is the container
// the wait is for.
func inspectContainers(ctx context.Context, cli hostIPContainerClient) ([]container.InspectResponse, error) {
	list, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}
	inspects := make([]container.InspectResponse, 0, len(list.Items))
	for _, item := range list.Items {
		result, err := cli.ContainerInspect(ctx, item.ID, client.ContainerInspectOptions{})
		if errdefs.IsNotFound(err) {
			continue // removed since the list
		}
		if err != nil {
			return nil, fmt.Errorf("failed to inspect container: %w", err)
		}
		inspects = append(inspects, result.Container)
	}
	return inspects, nil
}

// publishedHostIPs returns the specific host addresses the containers' port
// bindings name, in the form the config stores.
func publishedHostIPs(inspects []container.InspectResponse) []string {
	var addrs []netip.Addr
	for _, inspect := range inspects {
		if inspect.HostConfig == nil {
			continue
		}
		for _, bindings := range inspect.HostConfig.PortBindings {
			for _, binding := range bindings {
				if addr, ok := waitableHostIP(binding.HostIP); ok {
					addrs = append(addrs, addr)
				}
			}
		}
	}
	return hostIPStrings(addrs)
}

// assignedHostIPs returns the addresses the host holds, in the form the
// config stores. It is given addresses and no interface, so an address reads
// the same on a network card and on the bridge a handoff moved it to.
func assignedHostIPs(addrs []net.Addr) []string {
	var assigned []netip.Addr
	for _, value := range addrs {
		var ip net.IP
		switch v := value.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		addr, ok := netip.AddrFromSlice(ip)
		if !ok {
			continue
		}
		if addr, ok = waitableHostIP(addr); ok {
			assigned = append(assigned, addr)
		}
	}
	return hostIPStrings(assigned)
}

// waitableHostIP applies the rules of userconfig.ParseDockerHostIPs to an
// address that is already parsed: unmapped, and not unset, zoned, unspecified
// or loopback.
func waitableHostIP(addr netip.Addr) (netip.Addr, bool) {
	if !addr.IsValid() || addr.Zone() != "" {
		return netip.Addr{}, false
	}
	addr = addr.Unmap()
	if addr.IsUnspecified() || addr.IsLoopback() {
		return netip.Addr{}, false
	}
	return addr, true
}

// hostIPStrings sorts and de-duplicates addrs and returns their canonical
// text, never nil.
func hostIPStrings(addrs []netip.Addr) []string {
	slices.SortFunc(addrs, netip.Addr.Compare)
	addrs = slices.Compact(addrs)
	out := make([]string, len(addrs))
	for i, addr := range addrs {
		out[i] = addr.String()
	}
	return out
}
