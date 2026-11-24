package daemon

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

func dirSizePending(t *testing.T, d *daemon, path string) bool {
	t.Helper()
	rr := httptest.NewRecorder()
	d.handleDirSize(rr, httptest.NewRequest(http.MethodGet, "/dirsize?path="+url.QueryEscape(path), nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("dirsize of %s answered %d %q", path, rr.Code, rr.Body.String())
	}
	var resp api.DirSizeResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode dirsize: %v", err)
	}
	return resp.Pending
}

func subfoldersPending(t *testing.T, d *daemon, path string) bool {
	t.Helper()
	rr := httptest.NewRecorder()
	d.handleSubfolders(rr, httptest.NewRequest(http.MethodGet, "/subfolders?path="+url.QueryEscape(path), nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("subfolders of %s answered %d %q", path, rr.Code, rr.Body.String())
	}
	var resp api.SubfoldersResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode subfolders: %v", err)
	}
	return resp.Pending
}

func TestASizeAnswerSaysWhenAnUpdateForTheFolderStillWaits(t *testing.T) {
	d, _, folder := newDaemonWithIndexedFolder(t)
	sub := filepath.Join(folder, "sub")
	if dirSizePending(t, d, folder) || subfoldersPending(t, d, folder) {
		t.Fatal("a size is pending on a daemon with no work")
	}
	release := holdQueue(t, d)
	defer release()
	if dirSizePending(t, d, folder) {
		t.Fatal("a size is pending because of work that is not about a path")
	}

	// A file was added one folder down: the sizes of that folder and of the
	// folders above it are about to change.
	writeFile(t, filepath.Join(sub, "new.txt"))
	postAdd(d, filepath.Join(sub, "new.txt"))

	for _, path := range []string{sub, folder} {
		if !dirSizePending(t, d, path) {
			t.Errorf("the size of %s is not pending while the update waits", path)
		}
	}
	if !subfoldersPending(t, d, folder) {
		t.Error("the subfolder sizes are not pending while the update waits")
	}

	release()
	waitUntil(t, "the update is applied", func() bool { return !d.busy() })
	if dirSizePending(t, d, folder) || subfoldersPending(t, d, folder) {
		t.Fatal("a size is still pending after the update was applied")
	}
}

func TestASizeIsPendingWhileAFolderAboveItIsReindexed(t *testing.T) {
	d, _, folder := newDaemonWithIndexedFolder(t)
	release := holdQueue(t, d)
	defer release()

	postReindex(d, folder)

	if !dirSizePending(t, d, filepath.Join(folder, "sub")) {
		t.Fatal("the size of a folder is not pending while the reindex of the folder above it waits")
	}
}

func TestASizeIsNotPendingForWorkElsewhere(t *testing.T) {
	d, _, folder := newDaemonWithIndexedFolder(t)
	release := holdQueue(t, d)
	defer release()
	// A file beside the folder, and a refresh of the folder above it: neither
	// changes what is counted below sub.
	writeFile(t, filepath.Join(folder, "beside.txt"))
	postAdd(d, filepath.Join(folder, "beside.txt"))
	postRefresh(d, folder)

	if dirSizePending(t, d, filepath.Join(folder, "sub")) {
		t.Fatal("the size of a folder is pending for work that is not about it")
	}
}
