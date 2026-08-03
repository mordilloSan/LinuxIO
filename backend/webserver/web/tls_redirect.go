package web

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"
)

const (
	tlsRedirectIOTimeout   = 5 * time.Second
	maxRedirectRequestSize = 8 * 1024
)

// tlsRedirectListener wraps a net.Listener and peeks at each connection's
// first byte. TLS ClientHello messages start with 0x16; anything else is
// assumed to be plain HTTP and gets an automatic redirect to HTTPS.
type tlsRedirectListener struct {
	net.Listener
	tlsCfg   *tls.Config
	port     int
	done     chan struct{}
	close    sync.Once
	wg       sync.WaitGroup
	results  chan tlsRedirectResult
	activeMu sync.Mutex
	active   map[net.Conn]struct{}
}

type tlsRedirectResult struct {
	conn net.Conn
	err  error
}

// NewTLSRedirectListener returns a net.Listener that serves TLS for real TLS
// connections and returns an HTTP 301 redirect for plain-HTTP connections,
// all on the same port.
func NewTLSRedirectListener(inner net.Listener, tlsCfg *tls.Config, port int) net.Listener {
	l := &tlsRedirectListener{
		Listener: inner,
		tlsCfg:   tlsCfg,
		port:     port,
		done:     make(chan struct{}),
		results:  make(chan tlsRedirectResult),
		active:   make(map[net.Conn]struct{}),
	}
	l.wg.Add(1)
	go l.acceptLoop()
	return l
}

func (l *tlsRedirectListener) Accept() (net.Conn, error) {
	select {
	case result := <-l.results:
		select {
		case <-l.done:
			if result.conn != nil {
				_ = result.conn.Close()
			}
			return nil, net.ErrClosed
		default:
		}
		return result.conn, result.err
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *tlsRedirectListener) acceptLoop() {
	defer l.wg.Done()
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			select {
			case l.results <- tlsRedirectResult{err: err}:
				continue
			case <-l.done:
				return
			}
		}
		if !l.trackActive(conn) {
			_ = conn.Close()
			return
		}
		l.wg.Go(func() {
			defer l.removeActive(conn)
			l.classify(conn)
		})
	}
}

func (l *tlsRedirectListener) trackActive(conn net.Conn) bool {
	l.activeMu.Lock()
	defer l.activeMu.Unlock()
	select {
	case <-l.done:
		return false
	default:
		l.active[conn] = struct{}{}
		return true
	}
}

func (l *tlsRedirectListener) removeActive(conn net.Conn) {
	l.activeMu.Lock()
	delete(l.active, conn)
	l.activeMu.Unlock()
}

func (l *tlsRedirectListener) classify(conn net.Conn) {
	br := bufio.NewReader(conn)
	_ = conn.SetReadDeadline(time.Now().Add(tlsRedirectIOTimeout))
	first, err := br.Peek(1)
	if err != nil {
		conn.Close()
		return
	}
	_ = conn.SetReadDeadline(time.Time{})
	peeked := newPeekedConn(conn, br)
	if first[0] == 0x16 {
		select {
		case l.results <- tlsRedirectResult{conn: tls.Server(peeked, l.tlsCfg)}:
		case <-l.done:
			conn.Close()
		}
		return
	}
	l.redirectHTTP(peeked)
}

func (l *tlsRedirectListener) redirectHTTP(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(tlsRedirectIOTimeout))

	req, err := http.ReadRequest(bufio.NewReader(io.LimitReader(conn, maxRedirectRequestSize)))
	if err != nil {
		return
	}

	host := req.Host
	if host == "" {
		host = fmt.Sprintf("localhost:%d", l.port)
	}

	target := fmt.Sprintf("https://%s%s", host, req.RequestURI)
	body := fmt.Sprintf("<html><body>Redirecting to <a href=%q>%s</a></body></html>\n", target, target)

	resp := fmt.Sprintf(
		"HTTP/1.1 301 Moved Permanently\r\nLocation: %s\r\nContent-Type: text/html\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s",
		target, len(body), body,
	)
	if _, err := conn.Write([]byte(resp)); err != nil {
		slog.Debug("failed to write HTTP-to-HTTPS redirect",
			"address", conn.RemoteAddr(),
			"error", err)
		return
	}
}

func (l *tlsRedirectListener) Close() error {
	var err error
	l.close.Do(func() {
		close(l.done)
		err = l.Listener.Close()
		l.activeMu.Lock()
		for conn := range l.active {
			_ = conn.Close()
		}
		l.activeMu.Unlock()
		l.wg.Wait()
	})
	return err
}

// peekedConn wraps a net.Conn so that already-buffered bytes from the
// bufio.Reader are returned before reading from the underlying connection.
type peekedConn struct {
	net.Conn
	br *bufio.Reader
}

func newPeekedConn(c net.Conn, br *bufio.Reader) *peekedConn {
	return &peekedConn{Conn: c, br: br}
}

func (c *peekedConn) Read(b []byte) (int, error) {
	return c.br.Read(b)
}
