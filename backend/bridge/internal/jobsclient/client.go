// Package jobsclient connects to the caller's own job host, starting it
// first, and runs jobs on it: the bridge's side of a host connection and the
// "linuxio-bridge jobs submit" CLI.
package jobsclient

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
	"uuid"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/jobslauncher"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
	"github.com/mordilloSan/LinuxIO/backend/common/jobs"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

// ErrUpdatePending is the job host refusing this client's protocol: one of
// the two runs a binary from before an update.
var ErrUpdatePending = errors.New("update_pending")

const (
	hostWait = 5 * time.Second
	hostPoll = 50 * time.Millisecond
)

// Seams for tests.
var (
	startUnit = systemd.StartUnit
	isRoot    = func() bool { return os.Geteuid() == 0 }
	protocol  = jobs.Protocol
)

// Client is one connection to a job host, acting for the session of its hello.
type Client struct {
	session  *relay.YamuxSession
	protocol int
}

// Connect starts uid's job host, as root through systemd and otherwise
// through the launcher, connects to hostSocket and introduces hello, whose
// SessionRef the host uses as the connection's session ID.
func Connect(ctx context.Context, uid uint32, hello bridgeipc.HelloSession, launcherSocket, hostSocket string) (*Client, error) {
	if hello.SessionRef == "" {
		return nil, errors.New("job host hello needs a session ref")
	}
	if err := ensureHost(ctx, uid, launcherSocket); err != nil {
		return nil, err
	}
	conn, err := dialHost(ctx, hostSocket)
	if err != nil {
		return nil, err
	}
	sess, err := relay.NewYamuxClient(conn)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("start job host session: %w", err)
	}
	c := &Client{session: sess}
	data, err := c.Call(ctx, bridgeipc.HelloRoute, bridgeipc.Hello{Protocol: protocol, Session: hello})
	if taskErr, ok := errors.AsType[*bridgeipc.Error](err); ok && taskErr.Code == 426 {
		err = ErrUpdatePending
	}
	var answer bridgeipc.Hello
	if err == nil {
		if err = json.Unmarshal(data, &answer); err != nil {
			err = fmt.Errorf("decode job host hello: %w", err)
		}
	}
	if err != nil {
		_ = sess.Close()
		return nil, err
	}
	c.protocol = answer.Protocol
	return c, nil
}

// ensureHost starts uid's host: as root through systemd, otherwise through
// the launcher. StartUnit only queues the start, so the caller dials with
// retries.
func ensureHost(ctx context.Context, uid uint32, launcherSocket string) error {
	if !isRoot() {
		return launch(ctx, launcherSocket)
	}
	if err := startUnit(ctx, jobslauncher.UnitName(uid)); err != nil {
		return fmt.Errorf("start job host: %w", err)
	}
	return nil
}

// dialHost connects to hostSocket, retrying for hostWait while the host is
// still starting: the socket is missing, or a crashed host left a stale one
// that systemd has not removed yet.
func dialHost(ctx context.Context, hostSocket string) (net.Conn, error) {
	var dialer net.Dialer
	deadline := time.Now().Add(hostWait)
	for {
		conn, err := dialer.DialContext(ctx, "unix", hostSocket)
		if err == nil {
			return conn, nil
		}
		starting := errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED)
		if !starting || time.Now().After(deadline) {
			return nil, fmt.Errorf("dial job host: %w", err)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(hostPoll):
		}
	}
}

// launch asks the launcher to start this uid's host: it sends nothing and
// reads one line, "ok" or "error: <reason>".
func launch(ctx context.Context, socket string) error {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", socket)
	if err != nil {
		return fmt.Errorf("dial job launcher: %w", err)
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.SetReadDeadline(time.Now()) })
	defer stop()
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return fmt.Errorf("read job launcher reply: %w", cmp.Or(ctx.Err(), err))
	}
	if line = strings.TrimSuffix(line, "\n"); line != "ok" {
		return fmt.Errorf("job launcher refused: %s", strings.TrimPrefix(line, "error: "))
	}
	return nil
}

// Protocol is the jobs protocol the host answered the hello with.
func (c *Client) Protocol() int {
	return c.protocol
}

// Open opens a stream to the host and sends raw as its open frame payload.
func (c *Client) Open(ctx context.Context, raw []byte) (net.Conn, error) {
	stream, err := c.session.Open(ctx)
	if err != nil {
		return nil, fmt.Errorf("open job host stream: %w", err)
	}
	if err := relay.WriteRelayFrame(stream, &relay.StreamFrame{Opcode: relay.OpStreamOpen, Payload: raw}); err != nil {
		_ = stream.Close()
		return nil, fmt.Errorf("send stream open: %w", err)
	}
	return stream, nil
}

// Relay copies relay frames between from and to, both ways, until either
// side closes or fails or ctx ends, then closes both. A close or abort frame
// passes through like any other, so the host still sees a detach or a cancel.
func (c *Client) Relay(ctx context.Context, from, to net.Conn) error {
	errs := make(chan error, 2)
	pipe := func(dst, src net.Conn) {
		for {
			frame, err := relay.ReadRelayFrame(src)
			if err == nil {
				err = relay.WriteRelayFrame(dst, frame)
			}
			if err != nil {
				errs <- err
				return
			}
		}
	}
	closeBoth := func() {
		_ = from.Close()
		_ = to.Close()
	}
	stop := context.AfterFunc(ctx, closeBoth)
	defer stop()
	var pipes sync.WaitGroup
	pipes.Go(func() { pipe(to, from) })
	pipes.Go(func() { pipe(from, to) })
	err := <-errs
	closeBoth()
	pipes.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

// Done is closed when the connection to the host ends.
func (c *Client) Done() <-chan struct{} {
	return c.session.CloseChan()
}

func (c *Client) Close() error {
	return c.session.Close()
}

// Call opens route with request and returns the data of its result, or its
// error result as a *bridgeipc.Error.
func (c *Client) Call(ctx context.Context, route string, request any) (jsontext.Value, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encode %s request: %w", route, err)
	}
	raw, err := json.Marshal(relay.StreamOpenEnvelope{Route: route, Request: body})
	if err != nil {
		return nil, fmt.Errorf("encode %s open: %w", route, err)
	}
	stream, err := c.Open(ctx, raw)
	if err != nil {
		return nil, err
	}
	defer stream.Close()
	stop := context.AfterFunc(ctx, func() { _ = stream.SetReadDeadline(time.Now()) })
	defer stop()
	frame, err := relay.ReadRelayFrame(stream)
	if err == nil && frame.Opcode != relay.OpStreamResult {
		err = fmt.Errorf("unexpected frame 0x%02x", frame.Opcode)
	}
	if err != nil {
		return nil, fmt.Errorf("read %s result: %w", route, cmp.Or(ctx.Err(), err))
	}
	var result relay.ResultFrame
	if err := json.Unmarshal(frame.Payload, &result); err != nil {
		return nil, fmt.Errorf("decode %s result: %w", route, err)
	}
	if result.Status != "ok" {
		return nil, bridgeipc.NewError(result.Error, result.Code)
	}
	return result.Data, nil
}

// Submit starts a job of route with request on the host and prints its ID on
// a line of out. With wait it then prints a line per progress frame and the
// result, and returns exit code 0, or 1 after printing "error: <message>";
// without, it detaches once the job has started and returns 0.
func Submit(ctx context.Context, c *Client, route string, request jsontext.Value, wait bool, out io.Writer) (exitCode int, err error) {
	id := uuid.New().String()
	raw, err := json.Marshal(relay.StreamOpenEnvelope{Route: route, Request: request, ID: id})
	if err != nil {
		return 1, fmt.Errorf("encode job request: %w", err)
	}
	stream, err := c.Open(ctx, raw)
	if err != nil {
		return 1, err
	}
	defer stream.Close() // a close detaches; the job keeps running
	stop := context.AfterFunc(ctx, func() { _ = stream.SetReadDeadline(time.Now()) })
	defer stop()
	fmt.Fprintln(out, id)
	for {
		frame, err := relay.ReadRelayFrame(stream)
		if err != nil {
			return 1, fmt.Errorf("read job %s: %w", id, cmp.Or(ctx.Err(), err))
		}
		switch frame.Opcode {
		case relay.OpStreamProgress:
			fmt.Fprintln(out, progressLine(frame.Payload))
			if !wait {
				return 0, nil
			}
		case relay.OpStreamResult:
			var result relay.ResultFrame
			if err := json.Unmarshal(frame.Payload, &result); err != nil {
				return 1, fmt.Errorf("decode job %s result: %w", id, err)
			}
			if result.Status != "ok" {
				fmt.Fprintf(out, "error: %s\n", result.Error)
				return 1, nil
			}
			if len(result.Data) > 0 {
				fmt.Fprintln(out, string(result.Data))
			}
			return 0, nil
		}
	}
}

// progressLine is "phase: message" for a progress frame with a message, else
// its raw JSON (the task.snapshot frame included).
func progressLine(payload []byte) string {
	var p bridgeipc.TaskProgress
	if json.Unmarshal(payload, &p) != nil || p.Message == "" {
		return string(payload)
	}
	if p.Phase == "" {
		return p.Message
	}
	return p.Phase + ": " + p.Message
}

// Resume asks the root host to run the Resume hook of the interrupted
// resumable job id. It returns once the resumed job has started.
func Resume(ctx context.Context, c *Client, id string) error {
	_, err := c.Call(ctx, bridgeipc.ResumeRoute, map[string]string{"id": id})
	return err
}
