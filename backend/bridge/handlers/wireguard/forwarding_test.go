package wireguard

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// setInterfaceForwarding writes the interface's own forwarding switch, which
// the kernel makes when the interface comes up.
func setInterfaceForwarding(t *testing.T, name, value string) {
	t.Helper()
	path := interfaceForwardPath(name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// writeHookInterface writes an interface config with the given PostUp lines
// and no others.
func writeHookInterface(t *testing.T, name string, postUp ...string) {
	t.Helper()
	cfg := writeTestInterface(t, name, "10.7.0.1/24")
	cfg.Hooks = apischema.WireGuardHooks{apischema.WireGuardPostUp: postUp}
	if err := WriteWireGuardConfig(configPath(name), cfg); err != nil {
		t.Fatalf("WriteWireGuardConfig returned error: %v", err)
	}
}

// The sysctl line is refused by the profile Ubuntu confines wg-quick with,
// and sysctl exits 0 all the same: LinuxIO must not count on a hook for it.
func TestNATHooksLeaveForwardingToLinuxIO(t *testing.T) {
	for _, hook := range natPostUpHooks("eth0", "10.7.0.1/24") {
		if strings.Contains(hook, "sysctl") || strings.Contains(hook, "ip_forward") {
			t.Fatalf("PostUp hook sets a sysctl: %q", hook)
		}
	}
}

func TestUpTurnsForwardingOnBeforeTheUnitStarts(t *testing.T) {
	const unit = "wg-quick@wgtest.service"
	const oldHook = "sysctl -w net.ipv4.ip_forward=1"
	for _, tc := range []struct {
		name string
		// write makes wgtest's config.
		write func(t *testing.T)
		// forward is ip_forward before the up, and wantForward after it.
		forward, wantForward string
		wantBootFile         bool
	}{
		{
			name:    "an interface with NAT",
			write:   func(t *testing.T) { writeTestInterface(t, "wgtest", "10.7.0.1/24") },
			forward: "0\n", wantForward: "1\n", wantBootFile: true,
		},
		{
			// A write of ip_forward resets the per-interface settings, so
			// the value is left alone: it still has no line break here.
			name:    "a host that forwards already still gets the boot file",
			write:   func(t *testing.T) { writeTestInterface(t, "wgtest", "10.7.0.1/24") },
			forward: "1", wantForward: "1", wantBootFile: true,
		},
		{
			name:    "a config from before, with the sysctl hook",
			write:   func(t *testing.T) { writeHookInterface(t, "wgtest", oldHook) },
			forward: "0\n", wantForward: "1\n", wantBootFile: true,
		},
		{
			name:    "an interface without NAT is left alone",
			write:   func(t *testing.T) { writeHookInterface(t, "wgtest", "ip rule add from 10.7.0.0/24 table 7") },
			forward: "0\n", wantForward: "0\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			newWireGuardTestEnv(t)
			u := fakeUnit{state: "inactive"}
			u.install(t)
			tc.write(t)
			if err := os.WriteFile(ipForwardPath, []byte(tc.forward), 0o644); err != nil {
				t.Fatalf("write %s: %v", ipForwardPath, err)
			}
			// As on a host whose conf/default is off: the interface comes
			// up with its own switch off, whatever the host's says.
			setInterfaceForwarding(t, "wgtest", "0\n")

			if _, err := runWGQuick(context.Background(), "up", "wgtest"); err != nil {
				t.Fatalf("runWGQuick returned error: %v", err)
			}
			assertForwardingAfterUp(t, tc.wantForward, tc.wantBootFile)
			if want := []string{"start " + unit}; !slices.Equal(u.calls, want) {
				t.Fatalf("calls = %v, want %v", u.calls, want)
			}
		})
	}
}

// assertForwardingAfterUp checks the host's switch, the sysctl.d file and
// wgtest's own switch, which an up turns on only where the config needs it.
func assertForwardingAfterUp(t *testing.T, wantForward string, wanted bool) {
	t.Helper()
	if got := readTestFile(t, ipForwardPath); got != wantForward {
		t.Fatalf("ip_forward = %q, want %q", got, wantForward)
	}
	if !wanted {
		assertPathMissing(t, forwardSysctlPath)
	} else if got := readTestFile(t, forwardSysctlPath); !strings.Contains(got, "\nnet.ipv4.ip_forward = 1\n") {
		t.Fatalf("%s = %q, want the ip_forward line", forwardSysctlPath, got)
	}
	wantOwn := "0\n"
	if wanted {
		wantOwn = "1\n"
	}
	if got := readTestFile(t, interfaceForwardPath("wgtest")); got != wantOwn {
		t.Fatalf("the interface's own forwarding = %q, want %q", got, wantOwn)
	}
}

// The interface looks up and its peers connect: the list is the only place
// that says they reach nothing behind the server.
func TestListSaysWhenAnInterfaceThatIsUpDoesNotForward(t *testing.T) {
	for _, tc := range []struct {
		name      string
		nat, up   bool
		host, own string
		want      bool
	}{
		{name: "forwarding on", nat: true, up: true, host: "1\n", own: "1\n"},
		{name: "off on the host", nat: true, up: true, host: "0\n", own: "0\n", want: true},
		{name: "off on the interface alone", nat: true, up: true, host: "1\n", own: "0\n", want: true},
		{name: "an interface that is down", nat: true, host: "0\n", own: "0\n"},
		{name: "an interface without NAT", up: true, host: "0\n", own: "0\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newWireGuardTestEnv(t)
			env.interfaceUp = tc.up
			if tc.nat {
				writeTestInterface(t, "wgtest", "10.7.0.1/24")
			} else {
				writeHookInterface(t, "wgtest", "ip rule add from 10.7.0.0/24 table 7")
			}
			if err := os.WriteFile(ipForwardPath, []byte(tc.host), 0o644); err != nil {
				t.Fatalf("write %s: %v", ipForwardPath, err)
			}
			setInterfaceForwarding(t, "wgtest", tc.own)

			interfaces, err := handleListInterfaces(context.Background(), apischema.NoRequest{})
			if err != nil {
				t.Fatalf("list returned error: %v", err)
			}
			if len(interfaces) != 1 || interfaces[0].ForwardingOff != tc.want {
				t.Fatalf("interfaces = %+v, want one with ForwardingOff = %v", interfaces, tc.want)
			}
		})
	}
}

func TestRestoreForwarding(t *testing.T) {
	env := newWireGuardTestEnv(t)
	env.interfaceUp = true
	writeTestInterface(t, "wgtest", "10.7.0.1/24")
	setInterfaceForwarding(t, "wgtest", "0\n")

	if err := RestoreForwarding(context.Background(), apischema.NameRequest{Name: "wgtest"}); err != nil {
		t.Fatalf("RestoreForwarding returned error: %v", err)
	}
	if got := readTestFile(t, ipForwardPath); got != "1\n" {
		t.Fatalf("ip_forward = %q, want it on", got)
	}
	if got := readTestFile(t, interfaceForwardPath("wgtest")); got != "1\n" {
		t.Fatalf("the interface's own forwarding = %q, want it on", got)
	}
	assertPathExists(t, forwardSysctlPath)
	// The interface is neither started nor restarted: its peers stay connected.
	if len(env.wgQuick) != 0 {
		t.Fatalf("wg-quick calls = %v, want none", env.wgQuick)
	}

	err := RestoreForwarding(context.Background(), apischema.NameRequest{Name: "wgmissing"})
	if !errors.Is(err, bridgeipc.ErrNotFound) {
		t.Fatalf("error for a missing interface = %v, want ErrNotFound", err)
	}
	// Not a success that changed nothing.
	writeHookInterface(t, "wgplain", "ip rule add from 10.8.0.0/24 table 7")
	err = RestoreForwarding(context.Background(), apischema.NameRequest{Name: "wgplain"})
	if !errors.Is(err, bridgeipc.ErrConflict) {
		t.Fatalf("error for an interface without NAT = %v, want ErrConflict", err)
	}
	err = RestoreForwarding(context.Background(), apischema.NameRequest{Name: "../etc"})
	if !errors.Is(err, bridgeipc.ErrInvalidArgs) {
		t.Fatalf("error for a bad name = %v, want ErrInvalidArgs", err)
	}
}

// An interface whose peers would reach nothing is not started: the failure
// is the only place anyone is told.
func TestUpFailsWhenForwardingCannotBeTurnedOn(t *testing.T) {
	newWireGuardTestEnv(t)
	u := fakeUnit{state: "inactive"}
	u.install(t)
	writeTestInterface(t, "wgtest", "10.7.0.1/24")
	ipForwardPath = filepath.Join(t.TempDir(), "missing", "ip_forward")

	_, err := runWGQuick(context.Background(), "up", "wgtest")
	if err == nil || !strings.Contains(err.Error(), "turn on IPv4 forwarding for wgtest") {
		t.Fatalf("runWGQuick error = %v, want the forwarding failure", err)
	}
	if len(u.calls) != 0 {
		t.Fatalf("calls = %v, want none", u.calls)
	}
	// Nothing is kept for boot for an interface that did not start.
	assertPathMissing(t, forwardSysctlPath)
}

// An up leaves a working interface or none: callers read an error as "it
// is down", and the create and the edit say so.
func TestUpTakesDownAnInterfaceItCannotMakeForward(t *testing.T) {
	const unit = "wg-quick@wgtest.service"
	newWireGuardTestEnv(t)
	u := fakeUnit{state: "inactive"}
	u.install(t)
	writeTestInterface(t, "wgtest", "10.7.0.1/24")
	// No switch of its own to write: the fake start makes no interface.

	_, err := runWGQuick(context.Background(), "up", "wgtest")
	if err == nil || !strings.Contains(err.Error(), "turn on IPv4 forwarding on wgtest") {
		t.Fatalf("runWGQuick error = %v, want the interface's forwarding failure", err)
	}
	if want := []string{"start " + unit, "stop " + unit}; !slices.Equal(u.calls, want) {
		t.Fatalf("calls = %v, want %v", u.calls, want)
	}
}

func TestAddInterfaceWithoutIptablesSaysSoBeforeAnythingIsWritten(t *testing.T) {
	env := newWireGuardTestEnv(t)
	hasIptablesFunc = func() bool { return false }

	_, err := AddInterface(context.Background(), apischema.WireGuardAddInterfaceRequest{
		Name: "wgtest", Addresses: "10.7.0.1/24", ListenPort: "51820", EgressNic: "eth0", NumPeers: new("1"),
	})
	if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), "iptables is not installed") {
		t.Fatalf("AddInterface error = %v, want the missing iptables", err)
	}
	assertPathMissing(t, configPath("wgtest"))
	assertPathMissing(t, peerDirPath("wgtest"))
	if len(env.wgQuick) != 0 {
		t.Fatalf("wg-quick calls = %v, want none", env.wgQuick)
	}
}

func TestWantsForwarding(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind apischema.WireGuardHookKind
		line string
		want bool
	}{
		{name: "an iptables masquerade rule", kind: apischema.WireGuardPostUp, line: "iptables -t nat -A POSTROUTING -o eth0 -j MASQUERADE", want: true},
		{name: "an nft masquerade rule", kind: apischema.WireGuardPreUp, line: "nft add rule ip nat postrouting oifname eth0 masquerade", want: true},
		{name: "the sysctl line of a config from before", kind: apischema.WireGuardPostUp, line: "sysctl -w net.ipv4.ip_forward=1", want: true},
		{name: "IPv4 and IPv6 rules on one line", kind: apischema.WireGuardPostUp, line: "iptables -t nat -A POSTROUTING -o eth0 -j MASQUERADE; ip6tables -t nat -A POSTROUTING -o eth0 -j MASQUERADE", want: true},
		// IPv6 only: IPv4 forwarding is not what it asks for.
		{name: "an ip6tables masquerade rule", kind: apischema.WireGuardPostUp, line: "ip6tables -t nat -A POSTROUTING -o eth0 -j MASQUERADE"},
		{name: "a line that turns forwarding off", kind: apischema.WireGuardPostUp, line: "sysctl -w net.ipv4.ip_forward=0"},
		{name: "a rule deleted on the way down", kind: apischema.WireGuardPostDown, line: "iptables -t nat -D POSTROUTING -o eth0 -j MASQUERADE"},
		{name: "a line about something else", kind: apischema.WireGuardPostUp, line: "ip rule add from 10.7.0.0/24 table 7"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := WireGuardConfig{Hooks: apischema.WireGuardHooks{tc.kind: {tc.line}}}
			if got := wantsForwarding(cfg); got != tc.want {
				t.Fatalf("wantsForwarding(%q) = %v, want %v", tc.line, got, tc.want)
			}
		})
	}
}

// An interface LinuxIO did not bring up itself has no sysctl.d file: setting
// it to start at boot writes one, and leaves the running value alone.
func TestStartAtBootKeepsForwardingForBoot(t *testing.T) {
	newWireGuardTestEnv(t)
	writeTestInterface(t, "wgtest", "10.7.0.1/24")
	writeHookInterface(t, "wgplain", "ip rule add from 10.8.0.0/24 table 7")

	if err := ensureForwardingAtBoot("wgplain"); err != nil {
		t.Fatalf("ensureForwardingAtBoot returned error: %v", err)
	}
	assertPathMissing(t, forwardSysctlPath)

	if err := ensureForwardingAtBoot("wgtest"); err != nil {
		t.Fatalf("ensureForwardingAtBoot returned error: %v", err)
	}
	assertPathExists(t, forwardSysctlPath)
	if got := readTestFile(t, ipForwardPath); got != "0\n" {
		t.Fatalf("ip_forward = %q, want it left as it was", got)
	}
}

func TestUpdateInterfaceWithoutNATDropsTheBootFile(t *testing.T) {
	newWireGuardTestEnv(t)
	writeTestInterface(t, "wgtest", "10.7.0.1/24")
	if _, err := keepForwardingAtBoot(); err != nil {
		t.Fatalf("keepForwardingAtBoot returned error: %v", err)
	}

	updateInterface(t, apischema.WireGuardUpdateInterfaceRequest{Hooks: apischema.WireGuardHooks{
		apischema.WireGuardPostUp: {},
	}})
	assertPathMissing(t, forwardSysctlPath)
}

func TestRemoveDropsTheBootFileWithTheLastInterfaceThatNeedsIt(t *testing.T) {
	newWireGuardTestEnv(t)
	writeTestInterface(t, "wgtest", "10.7.0.1/24")
	writeTestInterface(t, "wgother", "10.8.0.1/24")
	if _, err := keepForwardingAtBoot(); err != nil {
		t.Fatalf("keepForwardingAtBoot returned error: %v", err)
	}
	if err := os.WriteFile(ipForwardPath, []byte("1\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", ipForwardPath, err)
	}

	if _, err := RemoveInterface(context.Background(), apischema.NameRequest{Name: "wgtest"}); err != nil {
		t.Fatalf("RemoveInterface returned error: %v", err)
	}
	assertPathExists(t, forwardSysctlPath)

	if _, err := RemoveInterface(context.Background(), apischema.NameRequest{Name: "wgother"}); err != nil {
		t.Fatalf("RemoveInterface returned error: %v", err)
	}
	assertPathMissing(t, forwardSysctlPath)
	// Docker, libvirt or the administrator may count on the running value.
	if got := readTestFile(t, ipForwardPath); got != "1\n" {
		t.Fatalf("ip_forward = %q after the removal, want it left on", got)
	}
}
