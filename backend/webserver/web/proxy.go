package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/mordilloSan/go-logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/webserver/bridge"
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

	proxyPrefix := "/proxy/" + containerName
	sess := session.SessionFromContext(r.Context())
	target, err := resolveContainerTarget(r.Context(), sess, containerName)
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
			req.Header.Set("X-Forwarded-Host", r.Host)
			req.Header.Set("X-Forwarded-Proto", forwardedProto(r))
			req.Header.Set("X-Forwarded-Prefix", proxyPrefix)
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

func forwardedProto(r *http.Request) string {
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		return proto
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

type proxyTargetResponse struct {
	URL string `json:"url"`
}

func resolveContainerTarget(ctx context.Context, sess *session.Session, name string) (*url.URL, error) {
	result, err := callBridgeJSON[proxyTargetResponse](ctx, sess, "docker", "resolve_proxy_target", name)
	if err != nil {
		return nil, err
	}
	if result.URL == "" {
		return nil, errors.New("empty proxy target")
	}
	return url.Parse(result.URL)
}

func callBridgeJSON[T any](ctx context.Context, sess *session.Session, handlerType, command string, args ...string) (T, error) {
	var zero T
	if sess == nil {
		return zero, errors.New("missing session")
	}

	yamuxSession, err := bridge.GetYamuxSession(sess.SessionID)
	if err != nil {
		return zero, fmt.Errorf("get bridge session: %w", err)
	}

	stream, err := yamuxSession.Open(ctx)
	if err != nil {
		return zero, fmt.Errorf("open bridge stream: %w", err)
	}
	defer func() {
		if cerr := stream.Close(); cerr != nil && !errors.Is(cerr, io.EOF) {
			logger.Debugf("[proxy] failed to close bridge stream: %v", cerr)
		}
	}()

	openArgs := append([]string{"bridge", handlerType, command}, args...)
	if err := ipc.WriteRelayFrame(stream, &ipc.StreamFrame{
		Opcode:  ipc.OpStreamOpen,
		Payload: []byte(strings.Join(openArgs, "\x00")),
	}); err != nil {
		return zero, fmt.Errorf("write bridge request: %w", err)
	}

	for {
		frame, err := ipc.ReadRelayFrame(stream)
		if err != nil {
			return zero, fmt.Errorf("read bridge response: %w", err)
		}

		switch frame.Opcode {
		case ipc.OpStreamResult:
			var result ipc.ResultFrame
			if err := json.Unmarshal(frame.Payload, &result); err != nil {
				return zero, fmt.Errorf("decode bridge result: %w", err)
			}
			if result.Status != "ok" {
				if result.Error == "" {
					return zero, errors.New("bridge request failed")
				}
				return zero, errors.New(result.Error)
			}
			if len(result.Data) == 0 {
				var empty T
				return empty, nil
			}

			var decoded T
			if err := json.Unmarshal(result.Data, &decoded); err != nil {
				return zero, fmt.Errorf("decode bridge payload: %w", err)
			}
			return decoded, nil
		case ipc.OpStreamClose:
			return zero, errors.New("bridge closed without result")
		}
	}
}
