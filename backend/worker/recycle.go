package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
	"github.com/mordilloSan/LinuxIO/backend/common/scheduledrun"
	"github.com/mordilloSan/LinuxIO/backend/common/sharedfolders"
)

const recycleUsage = "Usage: linuxio-worker recycle"

// loadRecycleBins and removeOlderThan are swapped by tests.
var (
	loadRecycleBins = sharedfolders.LoadRecycleBins
	removeOlderThan = btrfs.RemoveOlderThan
)

// runRecycle empties the recycle bin of every shared folder that has one of
// what has been in it for sharedfolders.RecycleBinMaxAge
// (docs/shared-folders.md, "Network"). Each bin is tried; one that could not
// be emptied is named on stderr and fails the run, whose end hook then
// raises the run alert.
func runRecycle(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, recycleUsage)
		return 2
	}
	bins, err := loadRecycleBins()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	cutoff := time.Now().Add(-sharedfolders.RecycleBinMaxAge)
	removed, failed := 0, 0
	for _, bin := range bins {
		reportProgress(-1, "Emptying the recycle bin of "+bin.Path)
		n, err := removeOlderThan(ctx, bin.UUID, bin.Path+"/"+sharedfolders.RecycleBinName, cutoff)
		removed += n
		switch {
		case btrfs.NotAFolder(err):
			// Anyone who may write the folder can put a link or a file
			// where its bin was: not followed, and not a failure, which
			// would raise the run's alert each night at a user's will.
			fmt.Fprintf(stderr, "%s on %s: skipped, its recycle bin is not a folder\n", bin.Path, bin.UUID)
		case err != nil:
			failed++
			fmt.Fprintf(stderr, "%s on %s: %v\n", bin.Path, bin.UUID, err)
		}
	}
	fmt.Fprintf(stdout, "removed=%d bins=%d errors=%d\n", removed, len(bins), failed)
	summary := scheduledrun.Summary{Message: "Nothing to do"}
	if removed > 0 {
		summary.Message = fmt.Sprintf("Recycle bins: %d removed", removed)
	}
	var unreported error
	if failed > 0 {
		unreported = fmt.Errorf("%d recycle bins could not be emptied", failed)
	}
	return scheduledrun.Conclude(ctx, summarize, summary, unreported)
}
