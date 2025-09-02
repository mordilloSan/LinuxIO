package version

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"sync"
)

var (
	Version   = "untracked"
	CommitSHA = "untracked"
	BuildTime = "unknown"

	shaOnce sync.Once
	shaHex  string
)

func SelfSHA256() string {
	shaOnce.Do(func() {
		exe, err := os.Executable()
		if err != nil {
			shaHex = "unknown"
			return
		}
		f, err := os.Open(exe)
		if err != nil {
			shaHex = "unknown"
			return
		}
		defer f.Close()

		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			shaHex = "unknown"
			return
		}
		shaHex = hex.EncodeToString(h.Sum(nil))
	})
	return shaHex
}
