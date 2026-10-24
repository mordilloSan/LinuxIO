package wireguard

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func ListPeers(ctx context.Context, req apischema.InterfaceNameRequest) ([]PeerInfo, error) {
	if req.InterfaceName == "" {
		return nil, fmt.Errorf("usage: list_exported_peers <interface>")
	}

	interfaceName := req.InterfaceName
	if err := validateInterfaceName(interfaceName); err != nil {
		return nil, fmt.Errorf("invalid interface name: %w", err)
	}
	slog.Debug("listing exported peers", "interface", interfaceName)

	peers, err := loadExportedPeers(ctx, interfaceName)
	if err != nil {
		return nil, fmt.Errorf("read exported peers of %s: %w", interfaceName, err)
	}

	statsByPub, err := loadPeerRuntimeStats(interfaceName)
	if err != nil {
		slog.Warn("failed to load runtime peer stats", "interface", interfaceName, "error", err)
		slog.Debug("listed exported peers", "interface", interfaceName, "count", len(peers))
		return peers, nil
	}
	applyPeerRuntimeStats(peers, statsByPub)
	slog.Debug("listed exported peers with runtime stats", "interface", interfaceName, "count", len(peers))
	return peers, nil
}

func loadExportedPeers(ctx context.Context, interfaceName string) ([]PeerInfo, error) {
	pattern := filepath.Join(peerDirPath(interfaceName), "*"+configExt)
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("list peer configs: %w", err)
	}

	peers := make([]PeerInfo, 0, len(files))
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		peer, err := readPeerFile(file)
		if err == nil {
			var pub string
			pub, err = peer.publicKey()
			if err == nil {
				peers = append(peers, PeerInfo{peerFile: peer, PublicKey: pub, LastHandshake: "never"})
				continue
			}
		}
		slog.Warn("failed to parse exported peer config", "path", file, "error", err)
	}

	return peers, nil
}

type peerRuntimeStats struct {
	LastHandshake     string
	LastHandshakeUnix int64
	RxBytes           int64
	TxBytes           int64
	RxBps             float64
	TxBps             float64
}

func loadPeerRuntimeStats(interfaceName string) (map[string]peerRuntimeStats, error) {
	client, err := wgctrl.New()
	if err != nil {
		return nil, err
	}
	defer client.Close()

	dev, err := client.Device(interfaceName)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	stats := make(map[string]peerRuntimeStats, len(dev.Peers))
	for _, peer := range dev.Peers {
		pub := peer.PublicKey.String()
		rxBps, txBps := computeRates(interfaceName, pub, peer.ReceiveBytes, peer.TransmitBytes, now)
		lastHandshakeUnix := peer.LastHandshakeTime.Unix()
		if peer.LastHandshakeTime.IsZero() {
			lastHandshakeUnix = 0
		}

		stats[pub] = peerRuntimeStats{
			LastHandshake:     formatPeerHandshake(peer.LastHandshakeTime),
			LastHandshakeUnix: lastHandshakeUnix,
			RxBytes:           peer.ReceiveBytes,
			TxBytes:           peer.TransmitBytes,
			RxBps:             rxBps,
			TxBps:             txBps,
		}
	}
	pruneRates(interfaceName, stats)

	return stats, nil
}

func formatPeerHandshake(ts time.Time) string {
	if ts.IsZero() {
		return "never"
	}
	return ts.UTC().Format(time.RFC3339)
}

func applyPeerRuntimeStats(peers []PeerInfo, statsByPub map[string]peerRuntimeStats) {
	for i := range peers {
		stats, ok := statsByPub[peers[i].PublicKey]
		if !ok {
			continue
		}
		peers[i].LastHandshake = stats.LastHandshake
		peers[i].LastHandshakeUnix = stats.LastHandshakeUnix
		peers[i].RxBytes = stats.RxBytes
		peers[i].TxBytes = stats.TxBytes
		peers[i].RxBps = stats.RxBps
		peers[i].TxBps = stats.TxBps
		peers[i].runtimeStatsKnown = true
	}
}

type rateSample struct {
	rx int64
	tx int64
	ts time.Time
}

var (
	rateMu    sync.Mutex
	rateCache = map[string]rateSample{} // key: iface + "|" + publicKey
)

func computeRates(iface, pub string, rx, tx int64, now time.Time) (rxBps, txBps float64) {
	key := iface + "|" + pub

	rateMu.Lock()
	defer rateMu.Unlock()

	prev, ok := rateCache[key]
	// store current sample immediately
	rateCache[key] = rateSample{rx: rx, tx: tx, ts: now}

	// first observation -> no rate yet
	if !ok {
		return 0, 0
	}

	dt := now.Sub(prev.ts).Seconds()
	if dt <= 0.4 { // avoid noisy very-short intervals
		return 0, 0
	}

	// counters can reset when interface bounces; guard negatives
	if rx < prev.rx || tx < prev.tx {
		return 0, 0
	}

	return float64(rx-prev.rx) / dt, float64(tx-prev.tx) / dt
}

// pruneRates drops the iface's cached samples for peers not in keep; a nil keep drops them all.
func pruneRates(iface string, keep map[string]peerRuntimeStats) {
	prefix := iface + "|"

	rateMu.Lock()
	defer rateMu.Unlock()

	for key := range rateCache {
		pub, ok := strings.CutPrefix(key, prefix)
		if _, kept := keep[pub]; ok && !kept {
			delete(rateCache, key)
		}
	}
}
