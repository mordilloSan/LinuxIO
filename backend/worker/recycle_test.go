package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
	"github.com/mordilloSan/LinuxIO/backend/common/scheduledrun"
	"github.com/mordilloSan/LinuxIO/backend/common/sharedfolders"
)

// stubRecycle stands in for the record and the pool: it records which bins
// were emptied, up to which time, and fails the bin named in fail.
func stubRecycle(t *testing.T, bins []sharedfolders.RecycleBin, fail string) (emptied *[]string, cutoffs *[]time.Time, stored *[]scheduledrun.Summary) {
	t.Helper()
	oldLoad, oldRemove, oldSummarize := loadRecycleBins, removeOlderThan, summarize
	t.Cleanup(func() { loadRecycleBins, removeOlderThan, summarize = oldLoad, oldRemove, oldSummarize })
	emptied, cutoffs, stored = &[]string{}, &[]time.Time{}, &[]scheduledrun.Summary{}
	loadRecycleBins = func() ([]sharedfolders.RecycleBin, error) { return bins, nil }
	removeOlderThan = func(_ context.Context, uuid, path string, cutoff time.Time) (int, error) {
		*emptied = append(*emptied, uuid+" "+path)
		*cutoffs = append(*cutoffs, cutoff)
		if path == fail {
			return 1, errors.New("pool offline")
		}
		return 2, nil
	}
	summarize = func(_ context.Context, s scheduledrun.Summary) error {
		*stored = append(*stored, s)
		return nil
	}
	return emptied, cutoffs, stored
}

func TestRecycleEmptiesTheBinOfEachFolderThatHasOne(t *testing.T) {
	bins := []sharedfolders.RecycleBin{{UUID: "u1", Path: "Media"}, {UUID: "u2", Path: "Docs"}}
	emptied, cutoffs, stored := stubRecycle(t, bins, "")
	var stdout, stderr bytes.Buffer
	before := time.Now()
	if code := runCLI(context.Background(), []string{"recycle"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if want := []string{"u1 Media/#recycle", "u2 Docs/#recycle"}; !slices.Equal(*emptied, want) {
		t.Errorf("emptied %v, want %v", *emptied, want)
	}
	// What has been in a bin for thirty days goes.
	for _, cutoff := range *cutoffs {
		if age := before.Sub(cutoff); age < 30*24*time.Hour-time.Minute || age > 30*24*time.Hour+time.Minute {
			t.Errorf("cut-off %v before the run, want thirty days", age)
		}
	}
	if stdout.String() != "removed=4 bins=2 errors=0\n" {
		t.Errorf("stdout = %q", stdout.String())
	}
	if len(*stored) != 1 || (*stored)[0].Message != "Recycle bins: 4 removed" {
		t.Errorf("summary = %+v", *stored)
	}
}

// A bin that could not be emptied fails the run, after the others were.
func TestRecycleFailsWhenABinCouldNotBeEmptied(t *testing.T) {
	bins := []sharedfolders.RecycleBin{{UUID: "u1", Path: "Media"}, {UUID: "u2", Path: "Docs"}}
	emptied, _, _ := stubRecycle(t, bins, "Media/#recycle")
	var stdout, stderr bytes.Buffer
	if code := runCLI(context.Background(), []string{"recycle"}, &stdout, &stderr); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if len(*emptied) != 2 {
		t.Errorf("emptied %v: the second bin was not tried", *emptied)
	}
	if !strings.Contains(stderr.String(), "Media on u1: pool offline") {
		t.Errorf("stderr = %q", stderr.String())
	}
}

func TestRecycleWithNoBin(t *testing.T) {
	_, _, stored := stubRecycle(t, nil, "")
	var stdout, stderr bytes.Buffer
	if code := runCLI(context.Background(), []string{"recycle"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if len(*stored) != 1 || (*stored)[0].Message != "Nothing to do" {
		t.Errorf("summary = %+v", *stored)
	}
}

func TestRecycleTakesNoArguments(t *testing.T) {
	stubRecycle(t, nil, "")
	var stdout, stderr bytes.Buffer
	if code := runCLI(context.Background(), []string{"recycle", "now"}, &stdout, &stderr); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
}

// Anyone who may write the folder can put a link or a file where its bin
// was. Neither is followed, and neither is a failure of the run: a run that
// failed each night would raise its alert each night, at a user's will.
func TestRecycleSkipsABinThatIsNotAFolder(t *testing.T) {
	for name, binErr := range map[string]error{
		"a link": fmt.Errorf("%w: Media/#recycle", btrfs.ErrLinkOnPath),
		"a file": &fs.PathError{Op: "open", Path: "/pool/Media/#recycle", Err: unix.ENOTDIR},
	} {
		t.Run(name, func(t *testing.T) {
			bins := []sharedfolders.RecycleBin{{UUID: "u1", Path: "Media"}, {UUID: "u2", Path: "Docs"}}
			emptied, _, _ := stubRecycle(t, bins, "")
			inner := removeOlderThan
			removeOlderThan = func(ctx context.Context, uuid, path string, cutoff time.Time) (int, error) {
				if uuid == "u1" {
					return 0, binErr
				}
				return inner(ctx, uuid, path, cutoff)
			}
			var stdout, stderr bytes.Buffer
			if code := runCLI(context.Background(), []string{"recycle"}, &stdout, &stderr); code != 0 {
				t.Fatalf("exit %d: %s", code, stderr.String())
			}
			if !strings.Contains(stderr.String(), "Media on u1: skipped") {
				t.Errorf("stderr = %q", stderr.String())
			}
			if len(*emptied) != 1 || stdout.String() != "removed=2 bins=2 errors=0\n" {
				t.Errorf("emptied %v, stdout %q", *emptied, stdout.String())
			}
		})
	}
}
