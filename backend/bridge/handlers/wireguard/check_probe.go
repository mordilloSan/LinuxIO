package wireguard

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/netip"
	"os"
	"runtime"
	"time"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// checkLinkName is the probe's WireGuard link while it is in the host's
// namespace, before it moves into its own. The dot keeps it out of the names
// LinuxIO accepts for an interface (validateInterfaceName).
const checkLinkName = "linuxio.check"

// kernelProbe is checkKernel on the running kernel. It runs no command: the
// job host's NoNewPrivileges keeps wg, ip and ping, which a distribution may
// confine, from changing profile (docs/wireguard.md).
type kernelProbe struct{}

func (kernelProbe) device(name string) (*wgtypes.Device, error) {
	client, err := wgctrl.New()
	if err != nil {
		return nil, fmt.Errorf("open wgctrl: %w", err)
	}
	defer client.Close()
	return client.Device(name)
}

func (kernelProbe) hostAddresses() ([]netip.Addr, error) {
	addrs, err := netlink.AddrList(nil, netlink.FAMILY_V4)
	if err != nil {
		return nil, err
	}
	own := make([]netip.Addr, 0, len(addrs))
	for _, addr := range addrs {
		if ip, ok := netip.AddrFromSlice(addr.IP); ok {
			own = append(own, ip.Unmap())
		}
	}
	return own, nil
}

// probe is a built probe: the temporary peer on the server's device and the
// probe's link in an anonymous namespace. The namespace lives as long as a
// handle or a socket in it is open, so a process that dies takes it, and the
// link in it, along.
type probe struct {
	plan      probePlan
	peerAdded bool
	ns        netns.NsHandle
	handle    *netlink.Handle
}

func (kernelProbe) open(ctx context.Context, plan probePlan) (probeEnv, error) {
	p := &probe{plan: plan, ns: netns.None()}
	if err := p.build(ctx); err != nil {
		// netlink and wgctrl have no context support; the cleanup runs on
		// its own deadline whatever ended the setup.
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
		defer cancel()
		return nil, errors.Join(err, p.close(cleanupCtx))
	}
	return p, nil
}

func (p *probe) build(ctx context.Context) error {
	client, err := wgctrl.New()
	if err != nil {
		return fmt.Errorf("open wgctrl: %w", err)
	}
	defer client.Close()

	// The temporary peer goes on the running device only, never in the
	// config, together with the removal of probe peers a crashed check left.
	peers := make([]wgtypes.PeerConfig, 0, len(p.plan.stale)+1)
	for _, key := range p.plan.stale {
		peers = append(peers, wgtypes.PeerConfig{PublicKey: key, Remove: true})
	}
	peers = append(peers, wgtypes.PeerConfig{
		PublicKey:         p.plan.privateKey.PublicKey(),
		ReplaceAllowedIPs: true,
		AllowedIPs:        []net.IPNet{hostNet(p.plan.probe)},
	})
	p.peerAdded = true
	if err = client.ConfigureDevice(p.plan.iface, wgtypes.Config{Peers: peers}); err != nil {
		return fmt.Errorf("add the temporary peer to %s: %w", p.plan.iface, err)
	}
	if err = ctx.Err(); err != nil {
		return err
	}

	if err = deleteCheckLink(); err != nil {
		return err
	}
	if p.ns, err = newNamespace(); err != nil {
		return fmt.Errorf("create a network namespace: %w", err)
	}
	// The link is created in the host's namespace, so its UDP socket stays
	// there after the move and 127.0.0.1 reaches the server's port.
	if err = netlink.LinkAdd(&netlink.Wireguard{Name: checkLinkName}); err != nil {
		return fmt.Errorf("create link %s: %w", checkLinkName, err)
	}
	if err = client.ConfigureDevice(checkLinkName, wgtypes.Config{
		PrivateKey:   &p.plan.privateKey,
		ReplacePeers: true,
		Peers: []wgtypes.PeerConfig{{
			PublicKey:  p.plan.serverKey,
			Endpoint:   &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: p.plan.listenPort},
			AllowedIPs: []net.IPNet{{IP: net.IPv4zero.To4(), Mask: net.CIDRMask(0, 32)}},
		}},
	}); err != nil {
		return fmt.Errorf("configure link %s: %w", checkLinkName, err)
	}
	link, err := netlink.LinkByName(checkLinkName)
	if err != nil {
		return fmt.Errorf("read link %s: %w", checkLinkName, err)
	}
	if err = netlink.LinkSetNsFd(link, int(p.ns)); err != nil {
		return fmt.Errorf("move link %s into the namespace: %w", checkLinkName, err)
	}
	if err = ctx.Err(); err != nil {
		return err
	}

	// NewHandleAt switches its thread into the namespace and back, ignoring a
	// failure to come back: it runs on a thread of its own, which is ended
	// rather than reused when it cannot be put back.
	if err = inNamespace(p.ns, func() error {
		var handleErr error
		p.handle, handleErr = netlink.NewHandleAt(p.ns, netlink.FAMILY_ALL)
		return handleErr
	}); err != nil {
		if p.handle != nil {
			p.handle.Close()
			p.handle = nil
		}
		return fmt.Errorf("open netlink in the namespace: %w", err)
	}
	return p.address()
}

// address brings lo and the link up in the namespace, gives the link the
// probe's address and sends everything through it.
func (p *probe) address() error {
	lo, err := p.handle.LinkByName("lo")
	if err != nil {
		return fmt.Errorf("read lo in the namespace: %w", err)
	}
	if err = p.handle.LinkSetUp(lo); err != nil {
		return fmt.Errorf("bring lo up in the namespace: %w", err)
	}
	link, err := p.handle.LinkByName(checkLinkName)
	if err != nil {
		return fmt.Errorf("read link %s in the namespace: %w", checkLinkName, err)
	}
	probeNet := hostNet(p.plan.probe)
	if err = p.handle.AddrAdd(link, &netlink.Addr{IPNet: &probeNet}); err != nil {
		return fmt.Errorf("add %s to the probe's link: %w", p.plan.probe, err)
	}
	if err = p.handle.LinkSetUp(link); err != nil {
		return fmt.Errorf("bring the probe's link up: %w", err)
	}
	route := &netlink.Route{
		LinkIndex: link.Attrs().Index,
		Dst:       &net.IPNet{IP: net.IPv4zero.To4(), Mask: net.CIDRMask(0, 32)},
		Src:       p.plan.probe.AsSlice(),
		Scope:     netlink.SCOPE_LINK,
	}
	if err = p.handle.RouteAdd(route); err != nil {
		return fmt.Errorf("add the probe's default route: %w", err)
	}
	return nil
}

func (p *probe) handshake(ctx context.Context, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	// The first packet through the link starts the handshake; its answer
	// does not matter here.
	if err := p.sendEcho(p.plan.probe, p.plan.tunnel); err != nil {
		return err
	}
	client, err := wgctrl.New()
	if err != nil {
		return fmt.Errorf("open wgctrl: %w", err)
	}
	defer client.Close()
	key := p.plan.privateKey.PublicKey()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		dev, err := client.Device(p.plan.iface)
		if err != nil {
			return fmt.Errorf("read device %s: %w", p.plan.iface, err)
		}
		for _, peer := range dev.Peers {
			if peer.PublicKey == key && !peer.LastHandshakeTime.IsZero() {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("no handshake on UDP port %d of 127.0.0.1 within %s", p.plan.listenPort, wait)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// sendEcho sends one echo request and does not wait for the reply.
func (p *probe) sendEcho(from, to netip.Addr) error {
	conn, err := p.dialICMP(from, to)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.Write(echoRequest(0, 0)); err != nil {
		return fmt.Errorf("send through the tunnel: %w", err)
	}
	return nil
}

func (p *probe) dialICMP(from, to netip.Addr) (*net.IPConn, error) {
	var conn *net.IPConn
	err := inNamespace(p.ns, func() error {
		var err error
		conn, err = net.DialIP("ip4:icmp", &net.IPAddr{IP: from.AsSlice()}, &net.IPAddr{IP: to.AsSlice()})
		return err
	})
	if err != nil {
		if conn != nil {
			_ = conn.Close()
		}
		return nil, fmt.Errorf("open an ICMP socket in the namespace: %w", err)
	}
	return conn, nil
}

func (p *probe) ping(ctx context.Context, target netip.Addr, wait time.Duration) error {
	conn, err := p.dialICMP(p.plan.probe, target)
	if err != nil {
		return err
	}
	return pingOver(ctx, conn, target, wait)
}

// pingOver sends echo requests to target on conn, which it closes, and
// waits for a reply.
func pingOver(ctx context.Context, conn *net.IPConn, target netip.Addr, wait time.Duration) error {
	defer conn.Close()
	id := randomID()
	// ReadFrom, not Read: it strips the IP header a raw socket reads.
	read := func(buf []byte) (int, error) {
		n, _, readErr := conn.ReadFrom(buf)
		return n, readErr
	}
	err := exchange(ctx, conn, echoRequest(id, 1), wait, read, func(msg []byte) bool { return isEchoReply(msg, id) })
	if errors.Is(err, errNoAnswer) {
		return fmt.Errorf("no echo reply from %s within %s", target, wait)
	}
	if err != nil && ctx.Err() == nil {
		return fmt.Errorf("ping %s: %w", target, err)
	}
	return err
}

func (p *probe) hostPing(ctx context.Context, target netip.Addr, wait time.Duration) error {
	// The job's own goroutine is in the host's namespace: the namespace is
	// entered only on threads of their own (inNamespace).
	conn, err := net.DialIP("ip4:icmp", nil, &net.IPAddr{IP: target.AsSlice()})
	if err != nil {
		return fmt.Errorf("open an ICMP socket: %w", err)
	}
	return pingOver(ctx, conn, target, wait)
}

func (p *probe) queryDNS(ctx context.Context, server netip.Addr, wait time.Duration) error {
	var conn *net.UDPConn
	err := inNamespace(p.ns, func() error {
		var err error
		conn, err = net.DialUDP("udp4", &net.UDPAddr{IP: p.plan.probe.AsSlice()}, &net.UDPAddr{IP: server.AsSlice(), Port: 53})
		return err
	})
	if err != nil {
		if conn != nil {
			_ = conn.Close()
		}
		return fmt.Errorf("open a UDP socket in the namespace: %w", err)
	}
	defer conn.Close()
	id := randomID()
	err = exchange(ctx, conn, dnsQuery(id), wait, conn.Read, func(msg []byte) bool { return isDNSReply(msg, id) })
	if errors.Is(err, errNoAnswer) {
		return fmt.Errorf("no answer from %s port 53 within %s", server, wait)
	}
	if err != nil && ctx.Err() == nil {
		// An ICMP error (port unreachable) reads as a refused connection.
		return fmt.Errorf("query %s: %w", server, err)
	}
	return err
}

var errNoAnswer = errors.New("no answer")

// resendAfter is when an exchange sends its request a second time, in case
// the first was lost.
const resendAfter = time.Second

// exchange sends request, again after resendAfter, and reads until answered
// takes a message or wait is over (errNoAnswer).
func exchange(ctx context.Context, conn net.Conn, request []byte, wait time.Duration, read func([]byte) (int, error), answered func([]byte) bool) error {
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()
	deadline := time.Now().Add(wait)
	buf := make([]byte, 4096)
	for attempt := 0; ; attempt++ {
		if _, err := conn.Write(request); err != nil {
			return errors.Join(ctx.Err(), err)
		}
		readUntil := deadline
		if resend := time.Now().Add(resendAfter); attempt == 0 && resend.Before(deadline) {
			readUntil = resend
		}
		found, err := readAnswer(ctx, conn, readUntil, buf, read, answered)
		if found || err != nil {
			return err
		}
		if attempt > 0 || !time.Now().Before(deadline) {
			return errNoAnswer
		}
	}
}

// readAnswer reads until answered takes a message (true) or until is over
// (false, no error).
func readAnswer(ctx context.Context, conn net.Conn, until time.Time, buf []byte, read func([]byte) (int, error), answered func([]byte) bool) (bool, error) {
	if err := conn.SetReadDeadline(until); err != nil {
		return false, err
	}
	for {
		n, err := read(buf)
		switch {
		case err == nil:
			if answered(buf[:n]) {
				return true, nil
			}
		case ctx.Err() != nil:
			return false, ctx.Err()
		case errors.Is(err, os.ErrDeadlineExceeded):
			return false, nil
		default:
			return false, err
		}
	}
}

// close removes the temporary peer, then the namespace and the link in it,
// and a link still in the host's namespace when the move did not happen.
func (p *probe) close(ctx context.Context) error {
	// netlink and wgctrl have no context support; ctx is accepted for
	// consistent handler flow.
	_ = ctx
	var errs []error
	if p.peerAdded {
		if err := removeDevicePeer(p.plan.iface, p.plan.privateKey.PublicKey()); err != nil {
			errs = append(errs, err)
		}
	}
	if p.handle != nil {
		if link, lookupErr := p.handle.LinkByName(checkLinkName); lookupErr == nil {
			if err := p.handle.LinkDel(link); err != nil {
				errs = append(errs, fmt.Errorf("delete the probe's link: %w", err))
			}
		}
		p.handle.Close()
		p.handle = nil
	}
	if p.ns.IsOpen() {
		if err := p.ns.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close the namespace: %w", err))
		}
		p.ns = netns.None()
	}
	if err := deleteCheckLink(); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func removeDevicePeer(iface string, key wgtypes.Key) error {
	client, err := wgctrl.New()
	if err != nil {
		return fmt.Errorf("open wgctrl: %w", err)
	}
	defer client.Close()
	err = client.ConfigureDevice(iface, wgtypes.Config{Peers: []wgtypes.PeerConfig{{PublicKey: key, Remove: true}}})
	if errors.Is(err, fs.ErrNotExist) {
		// The interface went down, and the peer with it.
		return nil
	}
	if err != nil {
		return fmt.Errorf("remove the temporary peer from %s: %w", iface, err)
	}
	return nil
}

// deleteCheckLink deletes a probe link left in the host's namespace by a
// check that died before it moved it. A link of that name that is not
// WireGuard is not LinuxIO's and is left alone.
func deleteCheckLink() error {
	link, err := netlink.LinkByName(checkLinkName)
	if err != nil {
		if _, ok := errors.AsType[netlink.LinkNotFoundError](err); ok {
			return nil
		}
		return fmt.Errorf("read link %s: %w", checkLinkName, err)
	}
	if link.Type() != "wireguard" {
		return fmt.Errorf("a %s link named %s is in the way", link.Type(), checkLinkName)
	}
	if err := netlink.LinkDel(link); err != nil {
		return fmt.Errorf("delete link %s: %w", checkLinkName, err)
	}
	return nil
}

// newNamespace creates an anonymous network namespace. Nothing names it, so
// it lasts only while the returned handle, or a socket in it, is open.
func newNamespace() (netns.NsHandle, error) {
	type result struct {
		ns  netns.NsHandle
		err error
	}
	done := make(chan result, 1)
	go func() {
		ns, err := onLockedThread(netns.New)
		done <- result{ns, err}
	}()
	r := <-done
	return r.ns, r.err
}

// inNamespace runs fn on a thread in ns, as sockets are created in the
// namespace of the thread that opens them; they stay in it afterwards. On an
// error the caller closes what fn opened.
func inNamespace(ns netns.NsHandle, fn func() error) error {
	done := make(chan error, 1)
	go func() {
		_, err := onLockedThread(func() (netns.NsHandle, error) {
			if err := netns.Set(ns); err != nil {
				return netns.None(), fmt.Errorf("enter the namespace: %w", err)
			}
			return netns.None(), fn()
		})
		done <- err
	}()
	return <-done
}

// onLockedThread runs fn on this goroutine's thread, locked, and puts the
// thread back in the namespace it started in. A thread that cannot be put
// back stays locked, so the runtime ends it with this goroutine instead of
// running other goroutines in the wrong namespace. It is called on a
// goroutine of its own for that reason.
func onLockedThread(fn func() (netns.NsHandle, error)) (netns.NsHandle, error) {
	runtime.LockOSThread()
	origin, err := netns.Get()
	if err != nil {
		runtime.UnlockOSThread()
		return netns.None(), fmt.Errorf("read the thread's namespace: %w", err)
	}
	defer origin.Close()
	ns, fnErr := fn()
	if err := netns.Set(origin); err != nil {
		if ns.IsOpen() {
			_ = ns.Close()
		}
		return netns.None(), errors.Join(fnErr, fmt.Errorf("return to the host's namespace: %w", err))
	}
	runtime.UnlockOSThread()
	return ns, fnErr
}

func hostNet(addr netip.Addr) net.IPNet {
	return net.IPNet{IP: addr.AsSlice(), Mask: net.CIDRMask(32, 32)}
}

func randomID() uint16 {
	var b [2]byte
	_, _ = rand.Read(b[:])
	return binary.BigEndian.Uint16(b[:])
}
