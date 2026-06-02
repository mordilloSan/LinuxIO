package dbusclient

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"

	godbus "github.com/godbus/dbus/v5"
)

type SignalMatch struct {
	Sender        string
	Interface     string
	Member        string
	Path          godbus.ObjectPath
	PathNamespace godbus.ObjectPath
	Args          map[int]string
	ArgPaths      map[int]string
	Arg0Namespace string
}

type SignalSubscription struct {
	ch    chan *godbus.Signal
	key   string
	match SignalMatch

	closeOnce sync.Once
	closeErr  error
}

type signalManager struct {
	mu        sync.Mutex
	conn      *godbus.Conn
	raw       chan *godbus.Signal
	subs      map[*SignalSubscription]struct{}
	matchRefs map[string]int
}

var signals = &signalManager{}

func WatchSignals(ctx context.Context, buffer int, match SignalMatch) (*SignalSubscription, error) {
	return signals.watch(ctx, buffer, match)
}

func WatchObjectSignals(ctx context.Context, path godbus.ObjectPath, buffer int, iface, member string) (*SignalSubscription, error) {
	return WatchSignals(ctx, buffer, SignalMatch{
		Interface: iface,
		Member:    member,
		Path:      path,
	})
}

func CloseSignals(ctx context.Context) error {
	return signals.close(ctx)
}

func (s *SignalSubscription) Chan() <-chan *godbus.Signal {
	if s == nil {
		return nil
	}
	return s.ch
}

func (s *SignalSubscription) Close(ctx context.Context) error {
	if s == nil {
		return nil
	}

	s.closeOnce.Do(func() {
		s.closeErr = signals.closeSubscription(ctx, s)
	})
	return s.closeErr
}

func (m SignalMatch) Options() []godbus.MatchOption {
	options := make([]godbus.MatchOption, 0, 4+len(m.Args)+len(m.ArgPaths))
	if m.Sender != "" {
		options = append(options, godbus.WithMatchSender(m.Sender))
	}
	if m.Interface != "" {
		options = append(options, godbus.WithMatchInterface(m.Interface))
	}
	if m.Member != "" {
		options = append(options, godbus.WithMatchMember(m.Member))
	}
	if m.Path != "" {
		options = append(options, godbus.WithMatchObjectPath(m.Path))
	}
	if m.PathNamespace != "" {
		options = append(options, godbus.WithMatchPathNamespace(m.PathNamespace))
	}
	for _, idx := range slices.Sorted(maps.Keys(m.Args)) {
		options = append(options, godbus.WithMatchArg(idx, m.Args[idx]))
	}
	for _, idx := range slices.Sorted(maps.Keys(m.ArgPaths)) {
		options = append(options, godbus.WithMatchArgPath(idx, m.ArgPaths[idx]))
	}
	if m.Arg0Namespace != "" {
		options = append(options, godbus.WithMatchArg0Namespace(m.Arg0Namespace))
	}
	return options
}

func (m SignalMatch) Matches(sig *godbus.Signal) bool {
	if sig == nil {
		return false
	}
	return m.matchesSender(sig) &&
		m.matchesName(sig) &&
		m.matchesPath(sig) &&
		m.matchesArgs(sig)
}

func (m SignalMatch) matchesSender(sig *godbus.Signal) bool {
	return m.Sender == "" || sig.Sender == m.Sender
}

func (m SignalMatch) matchesName(sig *godbus.Signal) bool {
	if m.Interface == "" && m.Member == "" {
		return true
	}
	iface, member := splitSignalName(sig.Name)
	if m.Interface != "" && iface != m.Interface {
		return false
	}
	return m.Member == "" || member == m.Member
}

func (m SignalMatch) matchesPath(sig *godbus.Signal) bool {
	if m.Path != "" && sig.Path != m.Path {
		return false
	}
	return m.PathNamespace == "" || pathInNamespace(sig.Path, m.PathNamespace)
}

func (m SignalMatch) matchesArgs(sig *godbus.Signal) bool {
	for idx, want := range m.Args {
		if !signalArgStringMatches(sig, idx, want) {
			return false
		}
	}
	for idx, want := range m.ArgPaths {
		if !signalArgPathMatches(sig, idx, want) {
			return false
		}
	}
	return m.Arg0Namespace == "" || signalArg0InNamespace(sig, m.Arg0Namespace)
}

func (m SignalMatch) key() string {
	parts := make([]string, 0, 8+len(m.Args)+len(m.ArgPaths))
	appendPart := func(key, value string) {
		if value != "" {
			parts = append(parts, key+"="+value)
		}
	}
	appendPart("sender", m.Sender)
	appendPart("interface", m.Interface)
	appendPart("member", m.Member)
	appendPart("path", string(m.Path))
	appendPart("path_namespace", string(m.PathNamespace))
	for _, idx := range slices.Sorted(maps.Keys(m.Args)) {
		parts = append(parts, "arg"+strconv.Itoa(idx)+"="+m.Args[idx])
	}
	for _, idx := range slices.Sorted(maps.Keys(m.ArgPaths)) {
		parts = append(parts, "arg"+strconv.Itoa(idx)+"path="+m.ArgPaths[idx])
	}
	appendPart("arg0namespace", m.Arg0Namespace)
	return strings.Join(parts, ";")
}

func (m *signalManager) watch(ctx context.Context, buffer int, match SignalMatch) (*SignalSubscription, error) {
	if buffer <= 0 {
		buffer = 20
	}
	ctx = requireContext(ctx)

	m.mu.Lock()
	defer m.mu.Unlock()

	conn, err := m.sharedSignalConnLocked()
	if err != nil {
		return nil, err
	}

	key := match.key()
	if key == "" {
		return nil, fmt.Errorf("empty D-Bus signal match")
	}
	if m.matchRefs[key] == 0 {
		if err := conn.AddMatchSignalContext(ctx, match.Options()...); err != nil {
			return nil, err
		}
	}
	m.matchRefs[key]++

	sub := &SignalSubscription{
		ch:    make(chan *godbus.Signal, buffer),
		key:   key,
		match: match,
	}
	m.subs[sub] = struct{}{}
	return sub, nil
}

func (m *signalManager) sharedSignalConnLocked() (*godbus.Conn, error) {
	if m.conn != nil && m.conn.Connected() {
		return m.conn, nil
	}

	conn, err := godbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("connect signal system bus: %w", err)
	}

	raw := make(chan *godbus.Signal, 100)
	conn.Signal(raw)

	m.conn = conn
	m.raw = raw
	m.subs = make(map[*SignalSubscription]struct{})
	m.matchRefs = make(map[string]int)
	go m.dispatch(raw)
	return conn, nil
}

func (m *signalManager) dispatch(raw <-chan *godbus.Signal) {
	for sig := range raw {
		m.dispatchSignal(sig)
	}
	m.closeSubscriptionsFromDispatcher(raw)
}

func (m *signalManager) dispatchSignal(sig *godbus.Signal) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for sub := range m.subs {
		if !sub.match.Matches(sig) {
			continue
		}
		select {
		case sub.ch <- sig:
		default:
			slog.Debug("dropping D-Bus signal for slow subscriber", "component", "dbus", "match", sub.key, "signal", sig.Name, "path", sig.Path)
		}
	}
}

func (m *signalManager) closeSubscription(ctx context.Context, sub *SignalSubscription) error {
	ctx = requireContext(ctx)

	m.mu.Lock()
	if _, ok := m.subs[sub]; !ok {
		m.mu.Unlock()
		return nil
	}

	delete(m.subs, sub)
	close(sub.ch)

	if m.conn == nil {
		m.mu.Unlock()
		return nil
	}

	m.matchRefs[sub.key]--
	if m.matchRefs[sub.key] > 0 {
		m.mu.Unlock()
		return nil
	}
	delete(m.matchRefs, sub.key)
	conn := m.conn
	options := sub.match.Options()
	m.mu.Unlock()

	return conn.RemoveMatchSignalContext(ctx, options...)
}

func (m *signalManager) close(ctx context.Context) error {
	ctx = requireContext(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}

	m.mu.Lock()
	for sub := range m.subs {
		sub.closeOnce.Do(func() {
			// Closing the shared connection drops the bus-side match rules, so
			// individual subscriptions intentionally retain a nil closeErr.
			close(sub.ch)
		})
	}
	m.subs = nil
	m.matchRefs = nil
	m.raw = nil

	if m.conn == nil {
		m.mu.Unlock()
		return nil
	}

	conn := m.conn
	m.conn = nil
	m.mu.Unlock()
	return conn.Close()
}

func (m *signalManager) closeSubscriptionsFromDispatcher(raw <-chan *godbus.Signal) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.raw != raw {
		return
	}

	for sub := range m.subs {
		sub.closeOnce.Do(func() {
			close(sub.ch)
		})
	}
	m.subs = nil
	m.matchRefs = nil
	m.raw = nil
	if m.conn != nil && !m.conn.Connected() {
		m.conn = nil
	}
}

func splitSignalName(name string) (string, string) {
	idx := strings.LastIndex(name, ".")
	if idx < 0 {
		return "", name
	}
	return name[:idx], name[idx+1:]
}

func pathInNamespace(path, namespace godbus.ObjectPath) bool {
	if namespace == "" {
		return true
	}
	if path == namespace {
		return true
	}
	p, ns := normalizeObjectPath(path), normalizeObjectPath(namespace)
	if ns == "/" {
		return strings.HasPrefix(p, "/")
	}
	return strings.HasPrefix(p, ns+"/")
}

func normalizeObjectPath(path godbus.ObjectPath) string {
	value := string(path)
	for len(value) > 1 && strings.HasSuffix(value, "/") {
		value = strings.TrimSuffix(value, "/")
	}
	return value
}

func signalArgStringMatches(sig *godbus.Signal, idx int, want string) bool {
	if idx < 0 || idx >= len(sig.Body) {
		return false
	}
	got, ok := sig.Body[idx].(string)
	return ok && got == want
}

func signalArgPathMatches(sig *godbus.Signal, idx int, want string) bool {
	if idx < 0 || idx >= len(sig.Body) {
		return false
	}
	switch got := sig.Body[idx].(type) {
	case godbus.ObjectPath:
		return string(got) == want
	case string:
		return got == want
	default:
		return false
	}
}

func signalArg0InNamespace(sig *godbus.Signal, namespace string) bool {
	if len(sig.Body) == 0 {
		return false
	}
	got, ok := sig.Body[0].(string)
	if !ok {
		return false
	}
	return got == namespace || strings.HasPrefix(got, namespace+".")
}
