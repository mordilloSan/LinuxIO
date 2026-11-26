package wireguard

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

// fakeProbe answers each probe from its maps: an address it does not hold
// gets no answer.
type fakeProbe struct {
	noHandshake bool
	answers     map[netip.Addr]bool // ping
	hostAnswers map[netip.Addr]bool // hostPing
	dnsAnswers  map[netip.Addr]bool
	asked       []string
	closed      bool
	cancelAt    string // cancels the check's context when this probe runs
	cancel      context.CancelFunc
}

func (f *fakeProbe) probe(what string, answered bool) error {
	f.asked = append(f.asked, what)
	if f.cancelAt == what {
		f.cancel()
		return context.Canceled
	}
	if !answered {
		return fmt.Errorf("no answer from %s", what)
	}
	return nil
}

func (f *fakeProbe) handshake(context.Context, time.Duration) error {
	return f.probe("handshake", !f.noHandshake)
}

func (f *fakeProbe) ping(_ context.Context, target netip.Addr, _ time.Duration) error {
	return f.probe("ping "+target.String(), f.answers[target])
}

func (f *fakeProbe) hostPing(_ context.Context, target netip.Addr, _ time.Duration) error {
	return f.probe("host ping "+target.String(), f.hostAnswers[target])
}

func (f *fakeProbe) queryDNS(_ context.Context, server netip.Addr, _ time.Duration) error {
	return f.probe("dns "+server.String(), f.dnsAnswers[server])
}

func (f *fakeProbe) close(context.Context) error {
	f.closed = true
	return nil
}

var (
	tunnelAddr  = netip.MustParseAddr("10.7.0.1")
	gatewayAddr = netip.MustParseAddr("192.0.2.1")
	outsideDNS  = netip.MustParseAddr("198.51.100.53")
)

func checkStatuses(steps []apischema.WireGuardCheckStep) string {
	out := ""
	for i, step := range steps {
		if i > 0 {
			out += " "
		}
		out += fmt.Sprintf("%s=%s", step.ID, step.Status)
	}
	return out
}

func runFakeCheck(t *testing.T, probe *fakeProbe, targets checkTargets) []apischema.WireGuardCheckStep {
	t.Helper()
	steps, err := runCheck(context.Background(), probe, targets)
	if err != nil {
		t.Fatalf("runCheck returned error: %v", err)
	}
	ids := make([]apischema.WireGuardCheckStepID, 0, len(steps))
	for _, step := range steps {
		ids = append(ids, step.ID)
	}
	if !slices.Equal(ids, apischema.WireGuardCheckStepIDs) {
		t.Fatalf("step order = %v, want %v", ids, apischema.WireGuardCheckStepIDs)
	}
	return steps
}

func TestCheckPassesEveryStep(t *testing.T) {
	probe := &fakeProbe{
		answers:    map[netip.Addr]bool{tunnelAddr: true, gatewayAddr: true},
		dnsAnswers: map[netip.Addr]bool{gatewayAddr: true},
	}
	steps := runFakeCheck(t, probe, checkTargets{tunnel: tunnelAddr, gateway: gatewayAddr, dns: []netip.Addr{gatewayAddr}})

	if got, want := checkStatuses(steps), "handshake=ok tunnel=ok beyond=ok dns=ok"; got != want {
		t.Fatalf("steps = %s, want %s", got, want)
	}
	if steps[1].Target != "10.7.0.1" || steps[2].Target != "192.0.2.1" || steps[3].Target != "192.0.2.1" {
		t.Fatalf("targets = %+v", steps)
	}
	// DNS is asked before "beyond" is decided.
	want := []string{"handshake", "ping 10.7.0.1", "dns 192.0.2.1", "ping 192.0.2.1"}
	if !slices.Equal(probe.asked, want) {
		t.Fatalf("asked %v, want %v", probe.asked, want)
	}
}

func TestCheckSkipsWhatAFailedStepLeavesUnproven(t *testing.T) {
	targets := checkTargets{tunnel: tunnelAddr, gateway: gatewayAddr, dns: []netip.Addr{gatewayAddr}}

	steps := runFakeCheck(t, &fakeProbe{noHandshake: true}, targets)
	if got, want := checkStatuses(steps), "handshake=failed tunnel=skipped beyond=skipped dns=skipped"; got != want {
		t.Fatalf("without a handshake: %s, want %s", got, want)
	}
	if steps[0].Detail == "" || steps[1].Detail == "" {
		t.Fatalf("a failed or skipped step says why: %+v", steps)
	}

	steps = runFakeCheck(t, &fakeProbe{answers: map[netip.Addr]bool{gatewayAddr: true}}, targets)
	if got, want := checkStatuses(steps), "handshake=ok tunnel=failed beyond=skipped dns=skipped"; got != want {
		t.Fatalf("without the tunnel: %s, want %s", got, want)
	}
}

// What a host that does not forward gives: the server answers, nothing
// behind it does.
func TestCheckFailsBeyondWhenNothingBehindTheServerAnswers(t *testing.T) {
	probe := &fakeProbe{
		answers:     map[netip.Addr]bool{tunnelAddr: true},
		hostAnswers: map[netip.Addr]bool{gatewayAddr: true},
	}
	steps := runFakeCheck(t, probe, checkTargets{tunnel: tunnelAddr, gateway: gatewayAddr, dns: []netip.Addr{gatewayAddr}})

	if got, want := checkStatuses(steps), "handshake=ok tunnel=ok beyond=failed dns=failed"; got != want {
		t.Fatalf("steps = %s, want %s", got, want)
	}
	if steps[2].Target != "192.0.2.1" || steps[2].Detail == "" {
		t.Fatalf("beyond = %+v", steps[2])
	}
	if steps[3].Target != "192.0.2.1" || steps[3].Detail == "" {
		t.Fatalf("dns = %+v", steps[3])
	}
}

func TestCheckBeyondPassesOnAnAnswerFromAnOutsideDNSServer(t *testing.T) {
	// A cloud router that never answers a ping.
	probe := &fakeProbe{
		answers:    map[netip.Addr]bool{tunnelAddr: true},
		dnsAnswers: map[netip.Addr]bool{outsideDNS: true},
	}
	steps := runFakeCheck(t, probe, checkTargets{tunnel: tunnelAddr, gateway: gatewayAddr, dns: []netip.Addr{outsideDNS}})
	if got, want := checkStatuses(steps), "handshake=ok tunnel=ok beyond=ok dns=ok"; got != want {
		t.Fatalf("steps = %s, want %s", got, want)
	}
	if steps[2].Target != outsideDNS.String() {
		t.Fatalf("beyond target = %q, want the DNS server", steps[2].Target)
	}

	// The same with no gateway at all.
	steps = runFakeCheck(t, probe, checkTargets{tunnel: tunnelAddr, dns: []netip.Addr{outsideDNS}})
	if got, want := checkStatuses(steps), "handshake=ok tunnel=ok beyond=ok dns=ok"; got != want {
		t.Fatalf("without a gateway: %s, want %s", got, want)
	}
}

// A resolver on the server itself answers without a packet leaving it.
func TestCheckBeyondIgnoresADNSServerOnThisHost(t *testing.T) {
	probe := &fakeProbe{
		answers:     map[netip.Addr]bool{tunnelAddr: true},
		hostAnswers: map[netip.Addr]bool{gatewayAddr: true},
		dnsAnswers:  map[netip.Addr]bool{tunnelAddr: true},
	}
	targets := checkTargets{tunnel: tunnelAddr, gateway: gatewayAddr, dns: []netip.Addr{tunnelAddr}, own: []netip.Addr{tunnelAddr}}
	steps := runFakeCheck(t, probe, targets)
	if got, want := checkStatuses(steps), "handshake=ok tunnel=ok beyond=failed dns=ok"; got != want {
		t.Fatalf("steps = %s, want %s", got, want)
	}

	targets.gateway = netip.Addr{}
	steps = runFakeCheck(t, probe, targets)
	if got, want := checkStatuses(steps), "handshake=ok tunnel=ok beyond=skipped dns=ok"; got != want {
		t.Fatalf("without a gateway: %s, want %s", got, want)
	}
	if steps[2].Detail == "" {
		t.Fatalf("a skipped beyond says why: %+v", steps[2])
	}
}

// A gateway that answers no ping at all (some cloud routers) proves nothing:
// its silence through the tunnel is a failure only when it answers the host.
func TestCheckBeyondJudgesASilentGatewayByTheHostsOwnPing(t *testing.T) {
	cases := []struct {
		name      string
		tunnel    bool
		host      bool
		dns       bool
		want      apischema.WireGuardCheckStatus
		wantAsked bool // whether the host was asked
	}{
		{"answers through the tunnel", true, true, false, apischema.WireGuardCheckOK, false},
		{"answers only the host", false, true, false, apischema.WireGuardCheckFailed, true},
		{"answers nobody, an outside DNS server did", false, false, true, apischema.WireGuardCheckOK, false},
		{"answers nobody, no outside DNS server did", false, false, false, apischema.WireGuardCheckSkipped, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			probe := &fakeProbe{
				answers:     map[netip.Addr]bool{tunnelAddr: true, gatewayAddr: tc.tunnel},
				hostAnswers: map[netip.Addr]bool{gatewayAddr: tc.host},
				dnsAnswers:  map[netip.Addr]bool{outsideDNS: tc.dns},
			}
			steps := runFakeCheck(t, probe, checkTargets{tunnel: tunnelAddr, gateway: gatewayAddr, dns: []netip.Addr{outsideDNS}})
			if steps[2].Status != tc.want {
				t.Fatalf("beyond = %+v, want %s", steps[2], tc.want)
			}
			if tc.want != apischema.WireGuardCheckOK && steps[2].Detail == "" {
				t.Fatalf("beyond says no why: %+v", steps[2])
			}
			if asked := slices.Contains(probe.asked, "host ping 192.0.2.1"); asked != tc.wantAsked {
				t.Fatalf("host asked = %v, want %v (asked %v)", asked, tc.wantAsked, probe.asked)
			}
		})
	}
}

func TestCheckSkipsDNSWithoutAServer(t *testing.T) {
	probe := &fakeProbe{answers: map[netip.Addr]bool{tunnelAddr: true}}
	steps := runFakeCheck(t, probe, checkTargets{tunnel: tunnelAddr})
	if got, want := checkStatuses(steps), "handshake=ok tunnel=ok beyond=skipped dns=skipped"; got != want {
		t.Fatalf("steps = %s, want %s", got, want)
	}
}

func TestCheckDNSNamesTheServerThatAnswered(t *testing.T) {
	second := netip.MustParseAddr("198.51.100.54")
	probe := &fakeProbe{
		answers:    map[netip.Addr]bool{tunnelAddr: true, gatewayAddr: true},
		dnsAnswers: map[netip.Addr]bool{second: true},
	}
	steps := runFakeCheck(t, probe, checkTargets{tunnel: tunnelAddr, gateway: gatewayAddr, dns: []netip.Addr{outsideDNS, second}})
	if steps[3].Status != apischema.WireGuardCheckOK || steps[3].Target != second.String() {
		t.Fatalf("dns = %+v, want ok from %s", steps[3], second)
	}
}

func TestCheckStopsWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	probe := &fakeProbe{answers: map[netip.Addr]bool{tunnelAddr: true}, cancelAt: "ping 10.7.0.1", cancel: cancel}
	_, err := runCheck(ctx, probe, checkTargets{tunnel: tunnelAddr, gateway: gatewayAddr})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("runCheck error = %v, want context.Canceled", err)
	}
}

func devicePeer(t *testing.T, allowed ...string) wgtypes.Peer {
	t.Helper()
	key, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatalf("GeneratePrivateKey: %v", err)
	}
	peer := wgtypes.Peer{PublicKey: key.PublicKey()}
	for _, value := range allowed {
		_, ipNet, err := net.ParseCIDR(value)
		if err != nil {
			t.Fatalf("ParseCIDR(%q): %v", value, err)
		}
		peer.AllowedIPs = append(peer.AllowedIPs, *ipNet)
	}
	return peer
}

func TestProbeAddressIsTheFirstNobodyHolds(t *testing.T) {
	subnet := netip.MustParsePrefix("10.7.0.1/24")
	configured := devicePeer(t, "10.7.0.2/32")
	cfg := WireGuardConfig{Peers: []PeerConfig{
		{PublicKey: configured.PublicKey.String(), AllowedIPs: []string{"10.7.0.2/32", "172.16.0.0/24"}},
	}}
	// A peer added by hand with two addresses, and one routing everything.
	byHand := devicePeer(t, "10.7.0.4/32", "10.7.0.5/32")
	everything := devicePeer(t, "0.0.0.0/0")
	device := []wgtypes.Peer{configured, byHand, everything}

	// 10.7.0.3 belongs to a disabled peer, kept only in its exported file.
	addr, stale, ok := pickProbeAddress(subnet, cfg, []string{"10.7.0.3/32"}, device)
	if !ok || addr != netip.MustParseAddr("10.7.0.6") || len(stale) != 0 {
		t.Fatalf("pickProbeAddress = %s, %v, %v; want 10.7.0.6 and nothing stale", addr, stale, ok)
	}
}

func TestProbeAddressTakesBackWhatACrashedCheckLeft(t *testing.T) {
	subnet := netip.MustParsePrefix("10.7.0.1/24")
	configured := devicePeer(t, "10.7.0.2/32")
	cfg := WireGuardConfig{Peers: []PeerConfig{{PublicKey: configured.PublicKey.String(), AllowedIPs: []string{"10.7.0.2/32"}}}}
	left := devicePeer(t, "10.7.0.3/32")

	addr, stale, ok := pickProbeAddress(subnet, cfg, nil, []wgtypes.Peer{configured, left})
	if !ok || addr != netip.MustParseAddr("10.7.0.3") {
		t.Fatalf("pickProbeAddress = %s, %v; want 10.7.0.3", addr, ok)
	}
	if !slices.Equal(stale, []wgtypes.Key{left.PublicKey}) {
		t.Fatalf("stale = %v, want the left peer", stale)
	}
	// A peer in the config with exactly that address is never stale.
	cfg.Peers = append(cfg.Peers, PeerConfig{PublicKey: left.PublicKey.String(), AllowedIPs: []string{"10.7.0.3/32"}})
	addr, stale, _ = pickProbeAddress(subnet, cfg, nil, []wgtypes.Peer{configured, left})
	if addr != netip.MustParseAddr("10.7.0.4") || len(stale) != 0 {
		t.Fatalf("pickProbeAddress = %s, %v; want 10.7.0.4 and nothing stale", addr, stale)
	}
}

func TestProbeAddressRunsOut(t *testing.T) {
	// A /30 has two hosts: the server and one peer.
	subnet := netip.MustParsePrefix("10.7.0.1/30")
	cfg := WireGuardConfig{Peers: []PeerConfig{{PublicKey: "x", AllowedIPs: []string{"10.7.0.2/32"}}}}
	if addr, _, ok := pickProbeAddress(subnet, cfg, nil, nil); ok {
		t.Fatalf("pickProbeAddress = %s, want none", addr)
	}
	if addr, _, ok := pickProbeAddress(subnet, WireGuardConfig{}, nil, nil); !ok || addr != netip.MustParseAddr("10.7.0.2") {
		t.Fatalf("pickProbeAddress = %s, %v; want 10.7.0.2", addr, ok)
	}
	if addr, _, ok := pickProbeAddress(netip.MustParsePrefix("10.7.0.1/32"), WireGuardConfig{}, nil, nil); ok {
		t.Fatalf("pickProbeAddress in a /32 = %s, want none", addr)
	}
}

// fakeKernel is checkKernel without a kernel.
type fakeKernel struct {
	dev     *wgtypes.Device
	devErr  error
	probe   *fakeProbe
	opened  []probePlan
	openErr error
}

func (k *fakeKernel) device(string) (*wgtypes.Device, error) { return k.dev, k.devErr }

func (k *fakeKernel) hostAddresses() ([]netip.Addr, error) {
	return []netip.Addr{tunnelAddr, netip.MustParseAddr("127.0.0.1")}, nil
}

func (k *fakeKernel) open(_ context.Context, plan probePlan) (probeEnv, error) {
	k.opened = append(k.opened, plan)
	if k.openErr != nil {
		return nil, k.openErr
	}
	return k.probe, nil
}

func useFakeKernel(t *testing.T, kernel *fakeKernel) {
	t.Helper()
	old := newCheckKernel
	newCheckKernel = func() checkKernel { return kernel }
	t.Cleanup(func() { newCheckKernel = old })
}

func TestCheckInterfaceRunsOnTheRunningDevice(t *testing.T) {
	newWireGuardTestEnv(t)
	cfg := writeTestInterface(t, "wgtest", "10.7.0.1/24")
	setTestInterfaceHeader(t, "wgtest", "", []string{"198.51.100.53", "search.example"})
	serverKey, err := wgtypes.ParseKey(cfg.PrivateKey)
	if err != nil {
		t.Fatalf("ParseKey: %v", err)
	}
	probe := &fakeProbe{
		answers:    map[netip.Addr]bool{tunnelAddr: true, gatewayAddr: true},
		dnsAnswers: map[netip.Addr]bool{outsideDNS: true},
	}
	kernel := &fakeKernel{dev: &wgtypes.Device{PublicKey: serverKey.PublicKey(), ListenPort: 51821}, probe: probe}
	useFakeKernel(t, kernel)

	result, err := CheckInterface(context.Background(), apischema.NameRequest{Name: "wgtest"})
	if err != nil {
		t.Fatalf("CheckInterface returned error: %v", err)
	}
	if got, want := checkStatuses(result.Steps), "handshake=ok tunnel=ok beyond=ok dns=ok"; got != want {
		t.Fatalf("steps = %s, want %s", got, want)
	}
	if !probe.closed {
		t.Fatal("the probe was not cleaned up")
	}
	plan := kernel.opened[0]
	// The running device's key and port, and the first free address.
	if plan.serverKey != serverKey.PublicKey() || plan.listenPort != 51821 || plan.probe != netip.MustParseAddr("10.7.0.2") || plan.tunnel != tunnelAddr {
		t.Fatalf("plan = %+v", plan)
	}
	// The search domain in the DNS line is not asked.
	if result.Steps[3].Target != "198.51.100.53" {
		t.Fatalf("dns target = %q", result.Steps[3].Target)
	}
}

func TestCheckInterfaceCleansUpWhenCancelled(t *testing.T) {
	newWireGuardTestEnv(t)
	cfg := writeTestInterface(t, "wgtest", "10.7.0.1/24")
	serverKey, _ := wgtypes.ParseKey(cfg.PrivateKey)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	probe := &fakeProbe{cancelAt: "handshake", cancel: cancel}
	useFakeKernel(t, &fakeKernel{dev: &wgtypes.Device{PublicKey: serverKey.PublicKey(), ListenPort: 51820}, probe: probe})

	if _, err := CheckInterface(ctx, apischema.NameRequest{Name: "wgtest"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("CheckInterface error = %v, want context.Canceled", err)
	}
	if !probe.closed {
		t.Fatal("a cancelled check was not cleaned up")
	}
}

func TestCheckInterfaceRefusals(t *testing.T) {
	newWireGuardTestEnv(t)
	writeTestInterface(t, "wgtest", "10.7.0.1/24")
	writeTestInterface(t, "wgsix", "fd00::1/64")
	kernel := &fakeKernel{devErr: fmt.Errorf("read: %w", fs.ErrNotExist)}
	useFakeKernel(t, kernel)

	cases := []struct {
		name string
		want error
	}{
		{"-bad", bridgeipc.ErrInvalidArgs},
		{"missing", bridgeipc.ErrNotFound},
		{"wgtest", bridgeipc.ErrConflict}, // down
		{"wgsix", bridgeipc.ErrConflict},  // no IPv4 address
	}
	for _, tc := range cases {
		if _, err := CheckInterface(context.Background(), apischema.NameRequest{Name: tc.name}); !errors.Is(err, tc.want) {
			t.Fatalf("CheckInterface(%s) error = %v, want %v", tc.name, err, tc.want)
		}
	}
	if len(kernel.opened) != 0 {
		t.Fatalf("a refused check built a probe: %+v", kernel.opened)
	}
}

// The check talks to the interface that is up, with that interface's key. One
// left running under another key than the config's would pass every step,
// while no device of the config completes a handshake with it.
func TestCheckInterfaceRefusesAnInterfaceRunningUnderAnotherKey(t *testing.T) {
	newWireGuardTestEnv(t)
	writeTestInterface(t, "wgtest", "10.7.0.1/24")
	other, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatalf("GeneratePrivateKey: %v", err)
	}
	kernel := &fakeKernel{dev: &wgtypes.Device{PublicKey: other.PublicKey(), ListenPort: 51820}}
	useFakeKernel(t, kernel)

	_, err = CheckInterface(context.Background(), apischema.NameRequest{Name: "wgtest"})
	if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), "another key than its config") {
		t.Fatalf("CheckInterface error = %v, want a conflict naming the key", err)
	}
	if len(kernel.opened) != 0 {
		t.Fatalf("a refused check built a probe: %+v", kernel.opened)
	}
}

// An interface whose own address is a /32 has no subnet to take a probe
// address from.
func TestCheckInterfaceWithAHostAddressIsRefused(t *testing.T) {
	newWireGuardTestEnv(t)
	cfg := writeTestInterface(t, "wgtest", "10.7.0.1/32")
	serverKey, _ := wgtypes.ParseKey(cfg.PrivateKey)
	kernel := &fakeKernel{dev: &wgtypes.Device{PublicKey: serverKey.PublicKey(), ListenPort: 51820}}
	useFakeKernel(t, kernel)

	_, err := CheckInterface(context.Background(), apischema.NameRequest{Name: "wgtest"})
	if !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), "no free address in 10.7.0.1/32") {
		t.Fatalf("CheckInterface error = %v, want a conflict naming the /32", err)
	}
	if len(kernel.opened) != 0 {
		t.Fatalf("a refused check built a probe: %+v", kernel.opened)
	}
}

func TestCheckLinkNameIsNoInterfaceName(t *testing.T) {
	if len(checkLinkName) > 15 {
		t.Fatalf("%s is longer than a link name may be", checkLinkName)
	}
	if validateInterfaceName(checkLinkName) == nil {
		t.Fatalf("%s is a name LinuxIO accepts for an interface", checkLinkName)
	}
}

func TestEchoRequestAndReply(t *testing.T) {
	req := echoRequest(0x1234, 7)
	if req[0] != icmpEchoRequest || icmpChecksum(req) != 0 {
		t.Fatalf("echo request %x does not check out", req)
	}
	reply := append([]byte(nil), req...)
	reply[0] = icmpEchoReply
	reply[2], reply[3] = 0, 0
	sum := icmpChecksum(reply)
	reply[2], reply[3] = byte(sum>>8), byte(sum)
	if !isEchoReply(reply, 0x1234) {
		t.Fatalf("reply %x not taken", reply)
	}
	if isEchoReply(reply, 0x1235) {
		t.Fatal("a reply to another id was taken")
	}
	if isEchoReply(req, 0x1234) {
		t.Fatal("the request itself was taken for a reply")
	}
	reply[9] ^= 0xff
	if isEchoReply(reply, 0x1234) {
		t.Fatal("a reply with a bad checksum was taken")
	}
	// RFC 1071's example, odd length padded with a zero byte.
	if got := icmpChecksum([]byte{0x00, 0x01, 0xf2, 0x03, 0xf4, 0xf5, 0xf6, 0xf7}); got != ^uint16(0xddf2) {
		t.Fatalf("checksum = %#x, want %#x", got, ^uint16(0xddf2))
	}
	if got := icmpChecksum([]byte{0x01}); got != ^uint16(0x0100) {
		t.Fatalf("odd-length checksum = %#x", got)
	}
}

func TestDNSQueryAndReply(t *testing.T) {
	query := dnsQuery(0xbeef)
	want := []byte{0xbe, 0xef, 0x01, 0x00, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 2, 0, 1}
	if !slices.Equal(query, want) {
		t.Fatalf("query = %x, want %x", query, want)
	}
	reply := append([]byte(nil), query...)
	reply[2] |= 0x80
	if !isDNSReply(reply, 0xbeef) {
		t.Fatal("a reply was not taken")
	}
	// REFUSED still answers: the server was reached.
	reply[3] = 0x85
	if !isDNSReply(reply, 0xbeef) {
		t.Fatal("a REFUSED reply was not taken")
	}
	if isDNSReply(reply, 0xbeee) {
		t.Fatal("a reply to another id was taken")
	}
	if isDNSReply(query, 0xbeef) {
		t.Fatal("the query itself was taken for a reply")
	}
	if isDNSReply(reply[:11], 0xbeef) {
		t.Fatal("a short message was taken")
	}
}
