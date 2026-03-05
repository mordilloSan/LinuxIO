package web

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"

	"github.com/mordilloSan/go-logger/logger"
)

// tlsRedirectListener wraps a net.Listener and peeks at each connection's
// first byte. TLS ClientHello messages start with 0x16; anything else is
// assumed to be plain HTTP and gets an automatic redirect to HTTPS.
type tlsRedirectListener struct {
	net.Listener
	tlsCfg *tls.Config
	port   int
}

// NewTLSRedirectListener returns a net.Listener that serves TLS for real TLS
// connections and returns an HTTP 301 redirect for plain-HTTP connections,
// all on the same port.
func NewTLSRedirectListener(inner net.Listener, tlsCfg *tls.Config, port int) net.Listener {
	return &tlsRedirectListener{Listener: inner, tlsCfg: tlsCfg, port: port}
}

func (l *tlsRedirectListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}

		br := bufio.NewReader(conn)
		// Peek at the first byte without consuming it.
		first, err := br.Peek(1)
		if err != nil {
			conn.Close()
			continue
		}

		peeked := newPeekedConn(conn, br)

		if first[0] == 0x16 {
			// TLS ClientHello — hand off to crypto/tls.
			return tls.Server(peeked, l.tlsCfg), nil
		}

		// Plain HTTP — send a redirect and close.
		go l.redirectHTTP(peeked)
	}
}

func (l *tlsRedirectListener) redirectHTTP(conn net.Conn) {
	defer conn.Close()

	req, err := http.ReadRequest(bufio.NewReader(conn))
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
		logger.Debugf("failed to write HTTP-to-HTTPS redirect to %v: %v", conn.RemoteAddr(), err)
		return
	}
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
