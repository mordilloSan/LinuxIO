package daemon

import (
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

func TestHandleAddSkipsExcludedPathBeforeOperationLock(t *testing.T) {
	d := &daemon{}
	d.running.Store(true)
	req := httptest.NewRequest(http.MethodPost, "/add", strings.NewReader(`{"path":"/proc/meminfo"}`))
	rr := httptest.NewRecorder()
	d.handleAdd(rr, req)

	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"status":"ok"`) {
		t.Fatalf("response = %d %q, want excluded-path success", rr.Code, rr.Body.String())
	}
}

func TestUnlockIndexStartsFreshIdleGrace(t *testing.T) {
	d := &daemon{}
	d.running.Store(true)
	d.activityMu.Lock()
	d.lastActivity = time.Now().Add(-idleGrace)
	d.activityMu.Unlock()

	d.activityMu.Lock()
	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		close(started)
		d.unlockIndex()
		close(done)
	}()
	<-started
	time.Sleep(10 * time.Millisecond)
	heldUntilActivityPublished := d.running.Load()
	d.activityMu.Unlock()
	<-done

	if !heldUntilActivityPublished {
		t.Fatal("operation pin released before activity was published")
	}
	if d.running.Load() {
		t.Fatal("operation pin remained active")
	}
	if idleFor := time.Since(d.activityTime()); idleFor > 100*time.Millisecond {
		t.Fatalf("operation release did not start a fresh grace: idle for %v", idleFor)
	}
}
