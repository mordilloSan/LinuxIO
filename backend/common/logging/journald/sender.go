package journald

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net"
	"os"
	"strings"
	"sync"
	"syscall"
)

const DefaultSocketPath = "/run/systemd/journal/socket"

// Field represents a native journald field.
type Field struct {
	Name  string
	Value string
}

// Sender transmits finalized journald fields.
type Sender interface {
	Send(fields []Field) error
}

// NewSender creates a native journald sender bound to the provided socket path.
func NewSender(socketPath string) (Sender, error) {
	addr := &net.UnixAddr{Name: socketPath, Net: "unixgram"}
	conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: "", Net: "unixgram"})
	if err != nil {
		return nil, err
	}

	return &nativeSender{
		addr: addr,
		conn: conn,
	}, nil
}

type nativeSender struct {
	mu   sync.Mutex
	addr *net.UnixAddr
	conn *net.UnixConn
	pool sync.Pool
}

func (s *nativeSender) Send(fields []Field) error {
	buf := s.getBuffer()
	defer s.putBuffer(buf)

	for _, field := range fields {
		if field.Name == "" {
			continue
		}
		appendField(buf, field.Name, field.Value)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, _, err := s.conn.WriteMsgUnix(buf.Bytes(), nil, s.addr); err == nil {
		return nil
	} else if !isSocketSpaceError(err) {
		return err
	}

	return s.sendLargePayload(buf.Bytes())
}

func (s *nativeSender) getBuffer() *bytes.Buffer {
	if v := s.pool.Get(); v != nil {
		if buf, ok := v.(*bytes.Buffer); ok {
			buf.Reset()
			return buf
		}
	}
	return &bytes.Buffer{}
}

func (s *nativeSender) putBuffer(buf *bytes.Buffer) {
	buf.Reset()
	s.pool.Put(buf)
}

func appendField(buf *bytes.Buffer, name, value string) {
	if strings.ContainsRune(value, '\n') {
		buf.WriteString(name)
		buf.WriteByte('\n')
		var size [8]byte
		binary.LittleEndian.PutUint64(size[:], uint64(len(value)))
		buf.Write(size[:])
		buf.WriteString(value)
		buf.WriteByte('\n')
		return
	}

	buf.WriteString(name)
	buf.WriteByte('=')
	buf.WriteString(value)
	buf.WriteByte('\n')
}

func (s *nativeSender) sendLargePayload(payload []byte) error {
	file, err := createTempPayloadFile()
	if err != nil {
		return err
	}
	defer file.Close()

	if _, writeErr := file.Write(payload); writeErr != nil {
		return writeErr
	}
	rights := syscall.UnixRights(int(file.Fd()))
	_, _, err = s.conn.WriteMsgUnix(nil, rights, s.addr)
	return err
}

func createTempPayloadFile() (*os.File, error) {
	for _, dir := range []string{"/dev/shm", ""} {
		file, err := os.CreateTemp(dir, "linuxio-journal-*")
		if err != nil {
			if dir == "" {
				return nil, err
			}
			continue
		}
		if removeErr := os.Remove(file.Name()); removeErr != nil {
			file.Close()
			return nil, removeErr
		}
		return file, nil
	}
	return nil, errors.New("failed to create journald payload file")
}

func isSocketSpaceError(err error) bool {
	var opErr *net.OpError
	if !errors.As(err, &opErr) || opErr == nil {
		return false
	}

	var sysErr *os.SyscallError
	if !errors.As(opErr.Err, &sysErr) || sysErr == nil {
		return false
	}

	return sysErr.Err == syscall.EMSGSIZE || sysErr.Err == syscall.ENOBUFS
}
