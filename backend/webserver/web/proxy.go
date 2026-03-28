package web

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/docker/docker/client"
	"github.com/mordilloSan/go-logger/logger"
)

const (
	proxyNetwork   = "linuxio-docker"
	proxyPortLabel = "io.linuxio.container.proxy.port"
)

// ContainerProxyHandler reverse-proxies requests at /proxy/{name}/...
// to the container's internal IP on the linuxio-docker bridge network.
// The container must have the io.linuxio.container.proxy.port label set.
func ContainerProxyHandler(w http.ResponseWriter, r *http.Request) {
	// Extract container name: /proxy/{name}[/rest]
	trimmed := strings.TrimPrefix(r.URL.Path, "/proxy/")
	slash := strings.IndexByte(trimmed, '/')
	var containerName, restPath string
	if slash < 0 {
		containerName = trimmed
		restPath = "/"
	} else {
		containerName = trimmed[:slash]
		restPath = trimmed[slash:]
	}

	if containerName == "" {
		http.Error(w, "container name required", http.StatusBadRequest)
		return
	}

	target, err := resolveContainerTarget(containerName)
	if err != nil {
		logger.Warnf("[proxy] cannot resolve %q: %v", containerName, err)
		http.Error(w, fmt.Sprintf("container %q not available: %v", containerName, err), http.StatusBadGateway)
		return
	}

	// Rewrite the request path — strip the /proxy/{name} prefix
	r2 := r.Clone(r.Context())
	r2.URL.Path = restPath
	r2.URL.RawPath = ""
	r2.URL.Scheme = target.Scheme
	r2.URL.Host = target.Host
	// Let the target host header pass through so apps that check Host work correctly
	r2.Host = target.Host

	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.Host = target.Host
		},
		// -1 enables streaming/flushing for SSE and WebSocket upgrades
		FlushInterval: -1,
		ErrorLog:      nil,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			logger.Warnf("[proxy] backend error for %q: %v", containerName, err)
			http.Error(w, "proxy error", http.StatusBadGateway)
		},
	}

	proxy.ServeHTTP(w, r2)
}

// resolveContainerTarget looks up the container by name, finds its IP on the
// linuxio-docker bridge, and returns the proxy target URL.
func resolveContainerTarget(name string) (*url.URL, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	defer func() {
		if cerr := cli.Close(); cerr != nil {
			logger.Warnf("[proxy] failed to close docker client: %v", cerr)
		}
	}()

	info, err := cli.ContainerInspect(context.Background(), name)
	if err != nil {
		return nil, fmt.Errorf("inspect container: %w", err)
	}

	port, ok := info.Config.Labels[proxyPortLabel]
	if !ok || port == "" {
		return nil, fmt.Errorf("label %s not set", proxyPortLabel)
	}

	// Prefer the container's IP on the linuxio-docker bridge
	ip := ""
	if nw, found := info.NetworkSettings.Networks[proxyNetwork]; found {
		ip = nw.IPAddress
	}
	if ip == "" {
		return nil, fmt.Errorf("container not connected to %s network", proxyNetwork)
	}

	return url.Parse(fmt.Sprintf("http://%s:%s", ip, port))
}
