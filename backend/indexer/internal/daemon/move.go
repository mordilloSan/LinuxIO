package daemon

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/mordilloSan/LinuxIO/backend/indexer/api"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/indexing"
)

// A move. The files are the same files, so the index renames the rows of the
// moved path where it can, instead of deleting them and walking the new
// folder to add everything again.

func (d *daemon) handleMove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "use POST", http.StatusMethodNotAllowed)
		return
	}
	var payload api.MoveRequest
	if err := json.UnmarshalRead(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes), &payload, json.RejectUnknownMembers(true)); err != nil {
		http.Error(w, fmt.Sprintf("invalid body: %v", err), http.StatusBadRequest)
		return
	}
	if payload.From == "" || payload.To == "" {
		http.Error(w, "from and to are required", http.StatusBadRequest)
		return
	}
	if !validateRelativePath(payload.From) || !validateRelativePath(payload.To) {
		http.Error(w, "invalid path: path traversal not allowed", http.StatusBadRequest)
		return
	}
	from, to := api.NormalizePath(payload.From), api.NormalizePath(payload.To)
	if from == "/" || to == "/" || from == to {
		http.Error(w, "from and to must be two paths below the root", http.StatusBadRequest)
		return
	}

	// Answered at once, on a free daemon too: the rows of a large folder take
	// a while to rename, and the file operation that moved it does not wait
	// for the index.
	move := &job{key: "move:" + from + "\x00" + to, paths: []string{from, to}, source: from}
	move.run = func(ctx context.Context) error {
		if err := d.moveEntry(ctx, from, to, move.sourceBusy); err != nil {
			slog.Warn("index update after a move failed", "from", from, "to", to, "error", err)
		}
		return nil
	}
	if queued, _ := d.enqueue(move); refused(queued) {
		http.Error(w, errShuttingDown.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSONStatus(w, http.StatusAccepted, api.OperationResponse{Status: api.StatusQueued})
}

// moveEntry brings the index up to date after a move. It renames the rows
// when what is at to is what the index holds at from. Otherwise the index
// follows the disk for both paths, as it did for every move before: the rows
// of a path that is gone are removed, and a folder is walked.
//
// sourceBusy is the job's: an update for a path below from that was still
// waiting when the folder moved names a path that is gone by the time it
// runs, and what it meant to add is found only by a walk of the new folder.
func (d *daemon) moveEntry(ctx context.Context, from, to string, sourceBusy bool) error {
	indexID, err := d.store.LatestIndexID(ctx)
	if err != nil {
		return fmt.Errorf("%w: %w", errNoIndex, err)
	}
	// Before the disk is touched: a path left out may be on a mount that no
	// longer answers.
	if !sourceBusy && !d.excludedFromIndex(from) && !d.excludedFromIndex(to) {
		renamed, renameErr := d.renameInIndex(ctx, indexID, from, to)
		if renameErr != nil {
			return fmt.Errorf("rename entries: %w", renameErr)
		}
		if renamed {
			return nil
		}
	}
	// Both, whatever the first one says: a source that cannot be looked at
	// must not keep what arrived at the new path out of the index.
	_, fromErr := d.reconcileEntry(ctx, from)
	_, toErr := d.reconcileEntry(ctx, to)
	return errors.Join(fromErr, toErr)
}

// renameInIndex renames the rows of from to to when the disk shows a move
// that is complete: nothing is left at from, and to is there. Whether to is
// what the index holds at from is the store's to say.
func (d *daemon) renameInIndex(ctx context.Context, indexID int64, from, to string) (bool, error) {
	// Never through a symlink: see indexing.Lstat.
	if _, err := indexing.Lstat(from); !indexing.NotThere(err) {
		// Still there, as after a move that stopped part-way, or unreadable.
		return false, nil
	}
	info, err := indexing.Lstat(to)
	if err != nil {
		return false, nil
	}
	return d.store.MovePath(ctx, indexID, from, indexing.EntryFromFileInfo(to, info))
}
