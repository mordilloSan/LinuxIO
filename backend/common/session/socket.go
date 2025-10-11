// session/socket.go
package session

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strconv"
)

const RuntimeBase = "/run/linuxio"

func SocketPath(uid uint32) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b[:]) // 32 hex chars
	dir := filepath.Join(RuntimeBase, strconv.FormatUint(uint64(uid), 10))
	return filepath.Join(dir, fmt.Sprintf("linuxio-bridge-%s.sock", token)), nil
}
