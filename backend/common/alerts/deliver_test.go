package alerts

import (
	"bufio"
	"context"
	"encoding/json/v2"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// webhookSink collects the JSON bodies posted to it.
func webhookSink(t *testing.T) (string, func() []map[string]string) {
	t.Helper()
	var mu sync.Mutex
	var got []map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.UnmarshalRead(r.Body, &body); err != nil {
			t.Errorf("decode webhook: %v", err)
		}
		mu.Lock()
		got = append(got, body)
		mu.Unlock()
	}))
	t.Cleanup(srv.Close)
	return srv.URL, func() []map[string]string {
		mu.Lock()
		defer mu.Unlock()
		return append([]map[string]string(nil), got...)
	}
}

func TestRaiseDeliversNewOccurrencesOnly(t *testing.T) {
	useTempStore(t)
	hook, received := webhookSink(t)
	if _, err := SaveNotifications(NotificationSettings{MinSeverity: SeverityWarning, WebhookURL: hook}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	raise := func(occurrence, severity string) {
		t.Helper()
		o := obs("disk")
		o.OccurrenceID, o.Severity = occurrence, severity
		if err := Raise(ctx, o); err != nil {
			t.Fatal(err)
		}
	}
	raise("a", SeverityError)
	raise("a", SeverityError)   // same occurrence: only observed again
	raise("b", SeverityError)   // new occurrence
	raise("c", SeverityInfo)    // below the minimum
	raise("d", SeverityWarning) // at the minimum

	got := received()
	if len(got) != 3 {
		t.Fatalf("deliveries = %d, want 3: %v", len(got), got)
	}
	if got[0]["severity"] != SeverityError || got[0]["source"] != "test" || got[0]["title"] == "" || !strings.Contains(got[0]["text"], "ERROR") {
		t.Fatalf("payload = %v", got[0])
	}
}

// fakeSMTP accepts one message without TLS or auth and returns it.
func fakeSMTP(t *testing.T) (int, <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	msg := make(chan string, 1)
	go func() {
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		serveSMTP(bufio.NewReader(conn), conn, msg)
	}()
	return tcpPort(t, ln), msg
}

func tcpPort(t *testing.T, ln net.Listener) int {
	t.Helper()
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener address %v is not TCP", ln.Addr())
	}
	return addr.Port
}

// serveSMTP answers one SMTP session and sends the message body on QUIT.
func serveSMTP(r *bufio.Reader, w io.Writer, msg chan<- string) {
	reply := func(s string) { _, _ = io.WriteString(w, s+"\r\n") }
	reply("220 fake")
	var data strings.Builder
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		switch cmd := strings.ToUpper(strings.TrimSpace(line)); {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			reply("250 fake")
		case strings.HasPrefix(cmd, "MAIL FROM:") || strings.HasPrefix(cmd, "RCPT TO:"):
			if strings.Count(cmd, "<") != 1 {
				reply("501 bad address")
			} else {
				reply("250 ok")
			}
		case cmd == "DATA":
			reply("354 go")
			readData(r, &data)
			reply("250 queued")
		case cmd == "QUIT":
			reply("221 bye")
			msg <- data.String()
			return
		default:
			reply("250 ok")
		}
	}
}

// readData copies the DATA lines up to the lone dot.
func readData(r *bufio.Reader, data *strings.Builder) {
	for {
		l, err := r.ReadString('\n')
		if err != nil || l == ".\r\n" {
			return
		}
		data.WriteString(l)
	}
}

func TestSendEmail(t *testing.T) {
	port, msg := fakeSMTP(t)
	n := NotificationSettings{SMTPHost: "127.0.0.1", SMTPPort: port, EmailFrom: "nas@example.com", EmailTo: []string{"me@example.com"}}
	a := TestAlert()
	a.Title = "Pool tank\r\nBcc: evil@example.com"
	if err := Send(context.Background(), n, a); err != nil {
		t.Fatal(err)
	}
	got := <-msg
	if !strings.Contains(got, "Subject: [LinuxIO ") || !strings.Contains(got, "Pool tank Bcc: evil@example.com\r\n") || strings.Contains(got, "\r\nBcc:") {
		t.Fatalf("message =\n%s", got)
	}
	if !strings.Contains(got, "To: me@example.com\r\n") || !strings.Contains(got, "Severity: info") {
		t.Fatalf("message =\n%s", got)
	}
}

func TestSendEmailEnvelopeUsesBareAddresses(t *testing.T) {
	port, msg := fakeSMTP(t)
	n := NotificationSettings{SMTPHost: "127.0.0.1", SMTPPort: port, EmailFrom: "LinuxIO <alerts@example.com>", EmailTo: []string{"Me <me@example.com>"}}
	if err := Send(context.Background(), n, TestAlert()); err != nil {
		t.Fatal(err)
	}
	got := <-msg
	if !strings.Contains(got, "From: LinuxIO <alerts@example.com>\r\n") || !strings.Contains(got, "To: Me <me@example.com>\r\n") {
		t.Fatalf("headers lost the display names:\n%s", got)
	}
}

func TestPostWebhookTransportErrorOmitsURL(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	target := srv.URL + "/services/T000/B000/secret-token"
	srv.Close() // nothing listens: the connection is refused
	err := postWebhook(context.Background(), target, TestAlert())
	if err == nil {
		t.Fatal("expected a transport error")
	}
	if strings.Contains(err.Error(), "secret-token") || strings.Contains(err.Error(), srv.URL) {
		t.Fatalf("error leaks the webhook URL: %v", err)
	}
}

func TestSendJoinsChannelErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }))
	defer srv.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := tcpPort(t, ln)
	_ = ln.Close() // nothing listens: the connection is refused
	n := NotificationSettings{WebhookURL: srv.URL, SMTPHost: "127.0.0.1", SMTPPort: port, EmailFrom: "a@b.c", EmailTo: []string{"d@e.f"}}
	err = Send(context.Background(), n, TestAlert())
	if err == nil || !strings.Contains(err.Error(), "webhook: ") || !strings.Contains(err.Error(), "502") || !strings.Contains(err.Error(), "email: ") {
		t.Fatalf("err = %v", err)
	}
}
