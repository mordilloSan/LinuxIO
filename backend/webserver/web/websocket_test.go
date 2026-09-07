package web

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

func newTestWebSocketPair(t testing.TB) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	ready := make(chan *websocket.Conn, 1)
	done := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		ready <- conn
		<-done
	}))
	t.Cleanup(func() { close(done); server.Close() })
	client, _, err := websocket.DefaultDialer.Dial(wsURL(server.URL, "/ws"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	_ = client.SetReadDeadline(time.Now().Add(time.Minute))
	return <-ready, client
}

type relayBenchmarkConn struct {
	net.Conn
	reader *bytes.Reader
}

func (c *relayBenchmarkConn) Read(p []byte) (int, error) { return c.reader.Read(p) }
func (c *relayBenchmarkConn) Close() error               { return nil }

// Measures framing, copies, allocations and loopback writes for a 1 MiB stream.
func BenchmarkRelayFromBridge(b *testing.B) {
	ws, client := newTestWebSocketPair(b)
	r := newStreamRelay(context.Background(), ws)
	b.Cleanup(r.closeAll)
	finished := make(chan error, 1)
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			_, reader, err := client.NextReader()
			if err != nil {
				select {
				case finished <- err:
				default:
				}
				return
			}
			var header [5]byte
			_, err = io.ReadFull(reader, header[:])
			if err == nil {
				_, err = io.Copy(io.Discard, reader)
			}
			if err != nil || header[4] == FlagFIN {
				finished <- err
			}
		}
	}()
	b.Cleanup(func() { _ = client.Close(); <-readerDone })
	payload := make([]byte, 1024*1024)
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	for b.Loop() {
		_, cancel := context.WithCancel(r.ctx)
		rs := &relayStream{id: 1, stream: &relayBenchmarkConn{reader: bytes.NewReader(payload)}, cancel: cancel}
		r.streams[rs.id] = rs
		r.relayFromBridge(rs)
		if err := <-finished; err != nil {
			b.Fatal(err)
		}
	}
}

func TestWebSocketRegistryConcurrentTabReplacement(t *testing.T) {
	const sessionID = "concurrent-tabs"
	for range 1000 {
		old, next := &websocket.Conn{}, &websocket.Conn{}
		addWebSocketForSession(sessionID, old)
		var wg sync.WaitGroup
		wg.Go(func() { removeWebSocketForSession(sessionID, old) })
		wg.Go(func() { addWebSocketForSession(sessionID, next) })
		wg.Wait()
		wsConnsBySession.Lock()
		_, registered := wsConnsBySession.conns[sessionID][next]
		wsConnsBySession.Unlock()
		removeWebSocketForSession(sessionID, next)
		if !registered {
			t.Fatal("new tab was lost during old tab removal")
		}
	}
}

func newTestYamuxPair(t *testing.T) (*relay.YamuxSession, *relay.YamuxSession) {
	t.Helper()
	left, right := net.Pipe()
	client, err := relay.NewYamuxClient(left)
	if err != nil {
		t.Fatal(err)
	}
	server, err := relay.NewYamuxServer(right)
	if err != nil {
		client.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close(); server.Close() })
	return client, server
}

func startTestRelayStream(t *testing.T, r *streamRelay, client, server *relay.YamuxSession, id uint32, payload []byte) net.Conn {
	t.Helper()
	ctx, cancel := context.WithCancel(r.ctx)
	stream, err := client.Open(ctx)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	peer, err := server.Accept()
	if err != nil {
		cancel()
		stream.Close()
		t.Fatal(err)
	}
	_ = peer.SetDeadline(time.Now().Add(3 * time.Second))
	rs := &relayStream{id: id, pending: make(chan relayWrite, streamWriteQueueSize), cancel: cancel}
	r.mu.Lock()
	r.streams[id] = rs
	r.mu.Unlock()
	r.wg.Go(func() { r.relayToBridge(ctx, rs, stream, payload) })
	t.Cleanup(func() { peer.Close() })
	return peer
}

func waitForRelayWorkers(t *testing.T, r *streamRelay) {
	t.Helper()
	done := make(chan struct{})
	go func() { r.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("relay workers did not stop")
	}
}

func TestRelayBlockedStreamDoesNotBlockOtherStreamsOrPongs(t *testing.T) {
	ws, client := newTestWebSocketPair(t)
	r := newStreamRelay(t.Context(), ws)
	yamuxClient, yamuxServer := newTestYamuxPair(t)
	blocked := startTestRelayStream(t, r, yamuxClient, yamuxServer, 1, nil)
	other := startTestRelayStream(t, r, yamuxClient, yamuxServer, 3, nil)
	sm := newTestSessionManager(session.DefaultConfig)
	t.Cleanup(sm.Close)
	sess, err := sm.CreateSession("relay-test", session.User{Username: "review"}, false)
	if err != nil {
		t.Fatal(err)
	}
	readerDone := make(chan struct{})
	go func() { defer close(readerDone); r.readLoop(sm, sess) }()
	t.Cleanup(func() { r.closeAll(); <-readerDone; waitForRelayWorkers(t, r) })

	// Exceed the initial yamux receive window, then stop consuming this stream.
	frame := make([]byte, 5+1024*1024)
	frame[3], frame[4] = 1, FlagDATA
	if err := client.WriteMessage(websocket.BinaryMessage, frame); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(blocked, make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	if err := client.WriteControl(websocket.PingMessage, []byte("alive"), time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := client.WriteMessage(websocket.BinaryMessage, []byte{0, 0, 0, 3, FlagDATA, 'o', 'k'}); err != nil {
		t.Fatal(err)
	}
	var data [2]byte
	if _, err := io.ReadFull(other, data[:]); err != nil || string(data[:]) != "ok" {
		t.Fatalf("unrelated stream stalled: %q, %v", data, err)
	}
	other.Close()
	ponged := false
	client.SetPongHandler(func(data string) error { ponged = data == "alive"; return nil })
	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := client.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	if !ponged {
		t.Fatal("blocked bridge stream prevented pong processing")
	}
	// The same blocked write must also stop promptly when its WebSocket closes.
	r.closeAll()
	waitForRelayWorkers(t, r)
}

func TestRelayQueueOverflowClosesOnlyTheSlowStream(t *testing.T) {
	ws, client := newTestWebSocketPair(t)
	r := newStreamRelay(t.Context(), ws)
	yamuxClient, yamuxServer := newTestYamuxPair(t)
	blocked := startTestRelayStream(t, r, yamuxClient, yamuxServer, 1, nil)
	other := startTestRelayStream(t, r, yamuxClient, yamuxServer, 3, nil)
	t.Cleanup(func() { r.closeAll(); waitForRelayWorkers(t, r) })
	r.enqueueFrame(1, FlagDATA, make([]byte, 1024*1024))
	if _, err := io.ReadFull(blocked, make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	for range streamWriteQueueSize + 1 {
		r.enqueueFrame(1, FlagDATA, []byte("queued"))
	}
	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	_, frame, err := client.ReadMessage()
	if err != nil || len(frame) != 5 || frame[3] != 1 || frame[4]&(FlagFIN|FlagRST) == 0 {
		t.Fatalf("slow stream did not close: %v, %v", frame, err)
	}
	r.enqueueFrame(3, FlagDATA, []byte("ok"))
	var data [2]byte
	if _, err := io.ReadFull(other, data[:]); err != nil || string(data[:]) != "ok" {
		t.Fatalf("overflow affected another stream: %q, %v", data, err)
	}
	if r.closed.Load() {
		t.Fatal("overflow closed the entire WebSocket")
	}
}

func TestRelayPreservesDataBeforeRST(t *testing.T) {
	ws, client := newTestWebSocketPair(t)
	r := newStreamRelay(t.Context(), ws)
	yamuxClient, yamuxServer := newTestYamuxPair(t)
	peer := startTestRelayStream(t, r, yamuxClient, yamuxServer, 1, []byte("open"))
	t.Cleanup(func() { r.closeAll(); waitForRelayWorkers(t, r) })
	r.enqueueFrame(1, FlagDATA, []byte("data"))
	r.enqueueFrame(1, FlagRST, nil)
	var data [8]byte
	if _, err := io.ReadFull(peer, data[:]); err != nil || string(data[:]) != "opendata" {
		t.Fatalf("close overtook earlier bytes: %q, %v", data, err)
	}
	if _, err := peer.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("expected EOF after queued data, got %v", err)
	}
	_, frame, err := client.ReadMessage()
	if err != nil || len(frame) != 5 || frame[4] != FlagRST {
		t.Fatalf("unexpected terminal frame: %v, %v", frame, err)
	}
}

func TestRelayWaitsForFINReply(t *testing.T) {
	ws, client := newTestWebSocketPair(t)
	r := newStreamRelay(t.Context(), ws)
	yamuxClient, yamuxServer := newTestYamuxPair(t)
	peer := startTestRelayStream(t, r, yamuxClient, yamuxServer, 1, []byte("open"))
	t.Cleanup(func() { r.closeAll(); waitForRelayWorkers(t, r) })
	r.enqueueFrame(1, FlagFIN, []byte("close"))
	var data [9]byte
	if _, err := io.ReadFull(peer, data[:]); err != nil || string(data[:]) != "openclose" {
		t.Fatalf("lost FIN payload: %q, %v", data, err)
	}
	if _, err := peer.Write([]byte("reply")); err != nil {
		t.Fatal(err)
	}
	_, frame, err := client.ReadMessage()
	if err != nil || len(frame) != 10 || frame[4] != FlagDATA || string(frame[5:]) != "reply" {
		t.Fatalf("lost FIN reply: %v, %v", frame, err)
	}
	peer.Close()
	_, frame, err = client.ReadMessage()
	if err != nil || len(frame) != 5 || frame[4] != FlagFIN {
		t.Fatalf("unexpected terminal frame: %v, %v", frame, err)
	}
}

func TestRelayWriteFailuresStopTheConnection(t *testing.T) {
	for _, ping := range []bool{false, true} {
		t.Run(map[bool]string{false: "data", true: "ping"}[ping], func(t *testing.T) {
			ws, _ := newTestWebSocketPair(t)
			r := newStreamRelay(t.Context(), ws)
			t.Cleanup(r.closeAll)
			_ = ws.NetConn().Close()
			if ping {
				if err := r.ping(); err == nil {
					t.Fatal("expected ping failure")
				}
			} else {
				reader := &relayBenchmarkConn{reader: bytes.NewReader(make([]byte, 1024*1024))}
				_, cancel := context.WithCancel(r.ctx)
				rs := &relayStream{id: 1, stream: reader, cancel: cancel}
				r.streams[rs.id] = rs
				r.relayFromBridge(rs)
				if reader.reader.Len() == 0 {
					t.Fatal("continued draining bridge data after a WebSocket write failure")
				}
			}
			if !r.closed.Load() || r.ctx.Err() == nil {
				t.Fatal("write failure left the relay running")
			}
		})
	}
}
