package auth

import (
	"net"
	"net/http"
	"net/netip"
	"os"
	"slices"
	"strings"
)

const trustedProxyCIDRsEnv = "LINUXIO_TRUSTED_PROXY_CIDRS"

var defaultTrustedProxyCIDRs = []string{
	"127.0.0.0/8",
	"::1/128",
	"172.16.0.0/12",
}

func clientRemoteHost(r *http.Request) string {
	peerHost, peerAddr, peerIsIP := remoteHostFromAddr(r.RemoteAddr)
	if peerIsIP && isTrustedProxy(peerAddr) {
		if forwarded := forwardedClientHost(r); forwarded != "" {
			return forwarded
		}
	}
	if peerHost == "" {
		return "web"
	}
	return peerHost
}

func forwardedClientHost(r *http.Request) string {
	trustedPrefixes := trustedProxyPrefixes()
	if host := bestForwardedFor(r.Header.Values("X-Forwarded-For"), trustedPrefixes); host != "" {
		return host
	}
	if host := untrustedForwardedIP(r.Header.Get("X-Real-IP"), trustedPrefixes); host != "" {
		return host
	}
	return bestRFCForwardedFor(r.Header.Values("Forwarded"), trustedPrefixes)
}

func remoteHostFromAddr(remoteAddr string) (string, netip.Addr, bool) {
	remoteAddr = strings.TrimSpace(remoteAddr)
	if remoteAddr == "" {
		return "", netip.Addr{}, false
	}

	if addrPort, err := netip.ParseAddrPort(remoteAddr); err == nil {
		addr := addrPort.Addr().Unmap()
		return addr.String(), addr, true
	}
	if addr, err := netip.ParseAddr(remoteAddr); err == nil {
		addr = addr.Unmap()
		return addr.String(), addr, true
	}
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		host = strings.Trim(strings.TrimSpace(host), "[]")
		if addr, err := netip.ParseAddr(host); err == nil {
			addr = addr.Unmap()
			return addr.String(), addr, true
		}
		return host, netip.Addr{}, false
	}
	return remoteAddr, netip.Addr{}, false
}

func bestForwardedFor(values []string, trustedPrefixes []netip.Prefix) string {
	addrs := make([]netip.Addr, 0)
	for _, value := range values {
		for part := range strings.SplitSeq(value, ",") {
			addr, ok := parseForwardedAddr(part)
			if ok {
				addrs = append(addrs, addr)
			}
		}
	}
	for _, v := range slices.Backward(addrs) {
		if !isTrustedProxyWithPrefixes(v, trustedPrefixes) {
			return v.String()
		}
	}
	return ""
}

func bestRFCForwardedFor(values []string, trustedPrefixes []netip.Prefix) string {
	addrs := make([]netip.Addr, 0)
	for _, value := range values {
		for entry := range strings.SplitSeq(value, ",") {
			for pair := range strings.SplitSeq(entry, ";") {
				key, rawValue, ok := strings.Cut(pair, "=")
				if !ok || !strings.EqualFold(strings.TrimSpace(key), "for") {
					continue
				}
				if addr, ok := parseForwardedAddr(rawValue); ok {
					addrs = append(addrs, addr)
				}
			}
		}
	}
	for _, v := range slices.Backward(addrs) {
		if !isTrustedProxyWithPrefixes(v, trustedPrefixes) {
			return v.String()
		}
	}
	return ""
}

func untrustedForwardedIP(value string, trustedPrefixes []netip.Prefix) string {
	addr, ok := parseForwardedAddr(value)
	if !ok || isTrustedProxyWithPrefixes(addr, trustedPrefixes) {
		return ""
	}
	return addr.String()
}

func parseForwardedAddr(value string) (netip.Addr, bool) {
	value = strings.Trim(strings.TrimSpace(value), `"`)
	if value == "" || strings.EqualFold(value, "unknown") {
		return netip.Addr{}, false
	}

	if addrPort, err := netip.ParseAddrPort(value); err == nil {
		return addrPort.Addr().Unmap(), true
	}
	if strings.HasPrefix(value, "[") {
		if end := strings.Index(value, "]"); end > 0 {
			if addr, err := netip.ParseAddr(value[1:end]); err == nil {
				return addr.Unmap(), true
			}
		}
	}
	if addr, err := netip.ParseAddr(value); err == nil {
		return addr.Unmap(), true
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		host = strings.Trim(strings.TrimSpace(host), "[]")
		if addr, err := netip.ParseAddr(host); err == nil {
			return addr.Unmap(), true
		}
	}
	return netip.Addr{}, false
}

func isTrustedProxy(addr netip.Addr) bool {
	return isTrustedProxyWithPrefixes(addr, trustedProxyPrefixes())
}

func isTrustedProxyWithPrefixes(addr netip.Addr, prefixes []netip.Prefix) bool {
	if !addr.IsValid() {
		return false
	}
	addr = addr.Unmap()
	for _, prefix := range prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func trustedProxyPrefixes() []netip.Prefix {
	cidrs := defaultTrustedProxyCIDRs
	if value, ok := os.LookupEnv(trustedProxyCIDRsEnv); ok {
		cidrs = strings.FieldsFunc(value, func(r rune) bool {
			return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\n'
		})
	}

	prefixes := make([]netip.Prefix, 0, len(cidrs))
	for _, cidr := range cidrs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(cidr))
		if err == nil {
			prefixes = append(prefixes, prefix)
		}
	}
	return prefixes
}
