package indexer

import (
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
	indexerapi "github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

// EntryForPath builds the path-only add request. The daemon owns the filesystem
// stat so callers cannot send contradictory metadata over the socket.
func EntryForPath(path string) indexerapi.EntryRequest {
	return indexerapi.EntryRequest{Path: utils.NormalizeIndexerPath(utils.CleanAbsPath(path))}
}
