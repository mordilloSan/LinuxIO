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
	if _, err := saveNotifications(t.Context(), NotificationSettings{MinSeverity: SeverityWarning, WebhookURL: hook}); err != nil {
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

func TestDeliveryFollowsTheGrid(t *testing.T) {
	for name, tc := range map[string]struct {
		severity         string
		ticks            Channels
		emails, webhooks int
	}{
		"a warning with only email": {severity: SeverityWarning, ticks: Channels{Browser: true, Email: true}, emails: 1},
		"a notice with nothing":     {severity: SeverityInfo, ticks: Channels{}},
		"an alert with both":        {severity: SeverityError, ticks: Channels{Browser: true, Email: true, Webhook: true}, emails: 1, webhooks: 1},
	} {
		t.Run(name, func(t *testing.T) {
			useTempStore(t)
			hook, posted := webhookSink(t)
			port, mailed := fakeSMTP(t)
			if _, err := saveNotifications(t.Context(), NotificationSettings{
				Levels: map[string]Channels{tc.severity: tc.ticks}, WebhookURL: hook,
				SMTPHost: "127.0.0.1", SMTPPort: port, EmailFrom: "nas@example.com", EmailTo: []string{"me@example.com"},
			}); err != nil {
				t.Fatal(err)
			}
			o := obs("disk")
			o.Severity = tc.severity
			if err := Raise(t.Context(), o); err != nil {
				t.Fatal(err)
			}
			if got := len(posted()); got != tc.webhooks {
				t.Errorf("webhooks = %d, want %d", got, tc.webhooks)
			}
			if got := len(mailed); got != tc.emails {
				t.Errorf("emails = %d, want %d", got, tc.emails)
			}
		})
	}
}

// A file an older build wrote names only the least severity sent: each level
// is delivered as that build delivered it.
func TestDeliveryFromAnOlderBuildsFile(t *testing.T) {
	for least, want := range map[string]int{"": 0, SeverityError: 1, SeverityWarning: 2, SeverityInfo: 3} {
		t.Run("minSeverity="+least, func(t *testing.T) {
			useTempStore(t)
			hook, posted := webhookSink(t)
			writeNotificationsFile(t, `{"minSeverity": "`+least+`", "webhookUrl": "`+hook+`"}`)
			for _, severity := range []string{SeverityInfo, SeverityWarning, SeverityError} {
				o := obs(severity)
				o.Severity = severity
				if err := Raise(t.Context(), o); err != nil {
					t.Fatal(err)
				}
			}
			if got := len(posted()); got != want {
				t.Fatalf("deliveries = %d, want %d", got, want)
			}
		})
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

// serveSMTP answers one SMTP session and sends the message body on QUIT,
// before its reply, so the message is there when the client's send returns.
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
			msg <- data.String()
			reply("221 bye")
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
	if err := Send(context.Background(), n, Channels{Email: true}, a); err != nil {
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
	if err := Send(context.Background(), n, Channels{Email: true}, TestAlert()); err != nil {
		t.Fatal(err)
	}
	got := <-msg
	if !strings.Contains(got, "From: LinuxIO <alerts@example.com>\r\n") || !strings.Contains(got, "To: Me <me@example.com>\r\n") {
		t.Fatalf("headers lost the display names:\n%s", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// A webhook's path or query can carry its token (Slack, Discord), and its
// errors reach the journal and the test button, so they name the host only.
func TestPostWebhookErrorsNameOnlyTheHost(t *testing.T) {
	const target = "https://hooks.example/services/T000/SECRET?token=SECRET"
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	refused := ln.Addr().String()
	_ = ln.Close() // nothing listens: the connection is refused
	for name, transport := range map[string]http.RoundTripper{
		"an answer that is not 2xx": roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusForbidden, Status: "403 Forbidden", Body: http.NoBody}, nil
		}),
		"a connection failure": &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, refused)
		}},
	} {
		t.Run(name, func(t *testing.T) {
			previous := http.DefaultClient.Transport
			http.DefaultClient.Transport = transport
			t.Cleanup(func() { http.DefaultClient.Transport = previous })
			err := postWebhook(t.Context(), target, TestAlert())
			if err == nil || !strings.Contains(err.Error(), "hooks.example") || strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "/services") {
				t.Fatalf("err = %v, want the host and nothing of the path or query", err)
			}
		})
	}
	// A URL that does not parse, as a hand edit may leave, is not quoted either.
	if err := postWebhook(t.Context(), target+"\x7f", TestAlert()); err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("err = %v, want one that quotes nothing of the URL", err)
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
	err = Send(context.Background(), n, Channels{Email: true, Webhook: true}, TestAlert())
	if err == nil || !strings.Contains(err.Error(), "webhook: ") || !strings.Contains(err.Error(), "502") || !strings.Contains(err.Error(), "email: ") {
		t.Fatalf("err = %v", err)
	}
}
