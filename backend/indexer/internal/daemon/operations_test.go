package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHandleAddRejectsLegacyEntryFields(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/add", strings.NewReader(`{"path":"/tmp/file","name":"file"}`))
	rr := httptest.NewRecorder()
	(&daemon{}).handleAdd(rr, req)

	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "unknown object member") {
		t.Fatalf("response = %d %q, want unknown-field rejection", rr.Code, rr.Body.String())
	}
}

func TestHandleAddRejectsRootAndDirectories(t *testing.T) {
	t.Run("root", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/add", strings.NewReader(`{"path":"/"}`))
		rr := httptest.NewRecorder()
		(&daemon{}).handleAdd(rr, req)
		if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "use /reindex") {
			t.Fatalf("response = %d %q, want reindex rejection", rr.Code, rr.Body.String())
		}
	})

	t.Run("directory", func(t *testing.T) {
		d, db := newDaemonWithDB(t)
		if _, err := db.Exec(`INSERT INTO indexes (last_indexed) VALUES (1)`); err != nil {
			t.Fatalf("insert index: %v", err)
		}
		path := t.TempDir()
		req := httptest.NewRequest(http.MethodPost, "/add", strings.NewReader(`{"path":"`+path+`"}`))
		rr := httptest.NewRecorder()
		d.handleAdd(rr, req)
		if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "use /reindex") {
			t.Fatalf("response = %d %q, want reindex rejection", rr.Code, rr.Body.String())
		}
	})
}

func TestHandleAddSkipsExcludedPathWithoutWaiting(t *testing.T) {
	d := &daemon{}
	release := holdQueue(t, d)
	defer release()
	req := httptest.NewRequest(http.MethodPost, "/add", strings.NewReader(`{"path":"/proc/meminfo"}`))
	rr := httptest.NewRecorder()
	d.handleAdd(rr, req)

	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"status":"ok"`) {
		t.Fatalf("response = %d %q, want excluded-path success", rr.Code, rr.Body.String())
	}
}

func TestFinishedWorkStartsFreshIdleGrace(t *testing.T) {
	d := &daemon{}
	d.activityMu.Lock()
	d.lastActivity = time.Now().Add(-idleGrace)
	d.activityMu.Unlock()

	// With the activity clock held, the work cannot publish its end: the
	// daemon must stay pinned until it has, or the idle watcher could see a
	// free daemon with a stale clock.
	d.activityMu.Lock()
	queued, _ := d.enqueue(&job{run: func(context.Context) error { return nil }})
	time.Sleep(20 * time.Millisecond)
	heldUntilActivityPublished := d.busy()
	d.activityMu.Unlock()
	<-queued.done

	if !heldUntilActivityPublished {
		t.Fatal("the daemon was free before the end of the work was published")
	}
	waitUntil(t, "the daemon is free again", func() bool { return !d.busy() })
	if idleFor := time.Since(d.activityTime()); idleFor > 100*time.Millisecond {
		t.Fatalf("finished work did not start a fresh grace: idle for %v", idleFor)
	}
}
