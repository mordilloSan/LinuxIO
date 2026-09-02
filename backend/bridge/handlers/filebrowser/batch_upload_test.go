package filebrowser

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgetasks "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
	ipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
	indexerapi "github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

func TestParseUploadBatchRequestValidation(t *testing.T) {
	valid := apischema.FileUploadBatchRequest{
		Destination: "/data/dest",
		Files: []apischema.FileUploadBatchEntry{
			{Path: "a.txt", Size: "5"},
			{Path: "sub/b.bin", Size: "0"},
		},
		Directories: []string{"empty/dir"},
	}

	destination, files, directories, total, err := parseUploadBatchRequest(valid)
	if err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	if destination != "/data/dest" {
		t.Fatalf("destination = %q", destination)
	}
	if len(files) != 2 || len(directories) != 1 {
		t.Fatalf("files = %d, directories = %d", len(files), len(directories))
	}
	if files[1].absPath != "/data/dest/sub/b.bin" {
		t.Fatalf("absPath = %q", files[1].absPath)
	}
	if total != 5 {
		t.Fatalf("total = %d, want 5", total)
	}

	invalid := []apischema.FileUploadBatchRequest{
		{Destination: "", Files: valid.Files},
		{Destination: "/data/dest"},
		{Destination: "/data/dest", Files: []apischema.FileUploadBatchEntry{{Path: "../escape", Size: "1"}}},
		{Destination: "/data/dest", Files: []apischema.FileUploadBatchEntry{{Path: "a/../../escape", Size: "1"}}},
		{Destination: "/data/dest", Files: []apischema.FileUploadBatchEntry{{Path: ".", Size: "1"}}},
		{Destination: "/data/dest", Files: []apischema.FileUploadBatchEntry{{Path: "a.txt", Size: "-1"}}},
		{Destination: "/data/dest", Files: []apischema.FileUploadBatchEntry{{Path: "a.txt", Size: "nope"}}},
		{Destination: "/data/dest", Directories: []string{".."}},
	}
	for i, req := range invalid {
		if _, _, _, _, err := parseUploadBatchRequest(req); err == nil {
			t.Errorf("invalid request %d accepted", i)
		}
	}
}

// startUploadBatchTask runs runUploadBatchTask on a fresh registry task and
// returns the task. The runner parks until a data stream drives it.
func startUploadBatchTask(t *testing.T, req apischema.FileUploadBatchRequest) *bridgetasks.Task {
	t.Helper()
	registry := bridgetasks.NewTaskService()
	task, err := registry.Create(routeUploadBatch, req)
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	task.Start(func(ctx context.Context, j *bridgetasks.Task, _ any) (any, error) {
		return runUploadBatchTask(ctx, j, req)
	})
	return task
}

// attachUploadBatchStream connects a client pipe to the task's data attacher
// and drains server frames in the background. It returns the client conn, a
// channel of received result frames, and a channel with the attacher's error.
func attachUploadBatchStream(t *testing.T, task *bridgetasks.Task, offset string) (net.Conn, <-chan ipc.ResultFrame, <-chan error) {
	t.Helper()
	serverConn, clientConn := net.Pipe()
	t.Cleanup(func() {
		_ = clientConn.Close()
		_ = serverConn.Close()
	})

	attachErr := make(chan error, 1)
	go func() {
		req := bridgetasks.TaskDataAttachRequest{}
		if offset != "" {
			req.Offset = &offset
		}
		attachErr <- attachFileTransferData(context.Background(), task, serverConn, req)
	}()

	results := make(chan ipc.ResultFrame, 4)
	go func() {
		for {
			frame, err := ipc.ReadRelayFrame(clientConn)
			if err != nil {
				close(results)
				return
			}
			if frame.Opcode == ipc.OpStreamResult {
				var result ipc.ResultFrame
				if err := json.Unmarshal(frame.Payload, &result); err == nil {
					results <- result
				}
			}
		}
	}()
	return clientConn, results, attachErr
}

func waitResult(t *testing.T, results <-chan ipc.ResultFrame) ipc.ResultFrame {
	t.Helper()
	select {
	case result, ok := <-results:
		if !ok {
			t.Fatal("stream closed before a result frame arrived")
		}
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for result frame")
	}
	return ipc.ResultFrame{}
}

func waitTaskDone(t *testing.T, task *bridgetasks.Task) bridgetasks.TaskSnapshot {
	t.Helper()
	select {
	case <-task.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for task to finish")
	}
	return task.Snapshot()
}

func writeData(t *testing.T, conn net.Conn, payload []byte) {
	t.Helper()
	if err := ipc.WriteRelayFrame(conn, &ipc.StreamFrame{Opcode: ipc.OpStreamData, Payload: payload}); err != nil {
		t.Fatalf("write data frame: %v", err)
	}
}

func writeClose(t *testing.T, conn net.Conn) {
	t.Helper()
	if err := ipc.WriteStreamClose(conn, 0); err != nil {
		t.Fatalf("write close frame: %v", err)
	}
}

type uploadBatchResult struct {
	Total       int    `json:"total"`
	Succeeded   int    `json:"succeeded"`
	Destination string `json:"destination"`
	Size        int64  `json:"size"`
	Failed      []struct {
		Path  string `json:"path"`
		Error string `json:"error"`
	} `json:"failed"`
}

func decodeUploadBatchResult(t *testing.T, result ipc.ResultFrame) uploadBatchResult {
	t.Helper()
	var decoded uploadBatchResult
	if err := json.Unmarshal(result.Data, &decoded); err != nil {
		t.Fatalf("decode result data: %v", err)
	}
	return decoded
}

func TestUploadBatchSingleStreamLandsAllFiles(t *testing.T) {
	dest := t.TempDir()
	contentA := []byte("hello")
	contentB := []byte(strings.Repeat("x", 300))

	req := apischema.FileUploadBatchRequest{
		Destination: dest,
		Files: []apischema.FileUploadBatchEntry{
			{Path: "a.txt", Size: "5"},
			{Path: "sub/deep/b.bin", Size: "300"},
			{Path: "empty.txt", Size: "0"},
		},
		Directories: []string{"emptydir/inner"},
	}

	task := startUploadBatchTask(t, req)
	conn, results, _ := attachUploadBatchStream(t, task, "")

	// All bytes in one frame: exercises boundary splitting inside one payload,
	// including the zero-size file between boundaries.
	writeData(t, conn, append(append([]byte{}, contentA...), contentB...))
	writeClose(t, conn)

	result := waitResult(t, results)
	if result.Status != "ok" {
		t.Fatalf("result status = %q (error %q)", result.Status, result.Error)
	}
	decoded := decodeUploadBatchResult(t, result)
	if decoded.Total != 4 || decoded.Succeeded != 4 || len(decoded.Failed) != 0 {
		t.Fatalf("result = %+v", decoded)
	}
	if decoded.Size != 305 {
		t.Fatalf("size = %d, want 305", decoded.Size)
	}

	snapshot := waitTaskDone(t, task)
	if snapshot.State != bridgetasks.TaskStateCompleted {
		t.Fatalf("task state = %q", snapshot.State)
	}

	for path, want := range map[string][]byte{
		"a.txt":          contentA,
		"sub/deep/b.bin": contentB,
		"empty.txt":      {},
	} {
		got, err := os.ReadFile(filepath.Join(dest, path))
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if string(got) != string(want) {
			t.Fatalf("%s content mismatch: got %d bytes", path, len(got))
		}
	}
	info, err := os.Stat(filepath.Join(dest, "emptydir/inner"))
	if err != nil || !info.IsDir() {
		t.Fatalf("manifest directory missing: %v", err)
	}

	entries, err := os.ReadDir(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".linuxio-upload-") {
			t.Fatalf("leftover temp buffer: %s", entry.Name())
		}
	}
}

func TestUploadBatchDirectoriesOnlyCompletesWithoutBytes(t *testing.T) {
	dest := t.TempDir()
	req := apischema.FileUploadBatchRequest{
		Destination: dest,
		Directories: []string{"only/dirs", "second"},
	}

	task := startUploadBatchTask(t, req)
	conn, results, _ := attachUploadBatchStream(t, task, "")
	writeClose(t, conn)

	result := waitResult(t, results)
	if result.Status != "ok" {
		t.Fatalf("result status = %q (error %q)", result.Status, result.Error)
	}
	decoded := decodeUploadBatchResult(t, result)
	if decoded.Total != 2 || decoded.Succeeded != 2 {
		t.Fatalf("result = %+v", decoded)
	}
	for _, dir := range []string{"only/dirs", "second"} {
		info, err := os.Stat(filepath.Join(dest, dir))
		if err != nil || !info.IsDir() {
			t.Fatalf("directory %s missing: %v", dir, err)
		}
	}
	waitTaskDone(t, task)
}

func TestUploadBatchCompletionReindexesDestinationOnce(t *testing.T) {
	detachedIndexerUpdates.Wait()
	dest := t.TempDir()
	requests := recordIndexerRequests(t)
	req := apischema.FileUploadBatchRequest{
		Destination: dest,
		Files: []apischema.FileUploadBatchEntry{
			{Path: "a.txt", Size: "1"},
			{Path: "sub/b.txt", Size: "1"},
		},
		Directories: []string{"sub"},
	}

	task := startUploadBatchTask(t, req)
	conn, results, _ := attachUploadBatchStream(t, task, "")
	writeData(t, conn, []byte("ab"))
	writeClose(t, conn)
	result := waitResult(t, results)
	if result.Status != "ok" {
		t.Fatalf("result status = %q (error %q)", result.Status, result.Error)
	}
	waitTaskDone(t, task)
	detachedIndexerUpdates.Wait()

	assertIndexerRequests(t, *requests, []recordedIndexerRequest{{
		method: http.MethodPost,
		path:   indexerapi.RouteReindex,
		query:  url.Values{"path": {dest}}.Encode(),
	}})
}

func TestUploadBatchSkipsFailedItemAndContinues(t *testing.T) {
	dest := t.TempDir()
	// "blocked" already exists as a directory: the upload of a file with that
	// name must fail per-item while the rest of the batch still lands.
	if err := os.Mkdir(filepath.Join(dest, "blocked"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	req := apischema.FileUploadBatchRequest{
		Destination: dest,
		Files: []apischema.FileUploadBatchEntry{
			{Path: "blocked", Size: "4"},
			{Path: "ok.txt", Size: "2"},
		},
	}

	task := startUploadBatchTask(t, req)
	conn, results, _ := attachUploadBatchStream(t, task, "")
	writeData(t, conn, []byte("XXXXok"))
	writeClose(t, conn)

	result := waitResult(t, results)
	if result.Status != "ok" {
		t.Fatalf("result status = %q (error %q)", result.Status, result.Error)
	}
	decoded := decodeUploadBatchResult(t, result)
	if decoded.Succeeded != 1 || len(decoded.Failed) != 1 {
		t.Fatalf("result = %+v", decoded)
	}
	if decoded.Failed[0].Path != "blocked" {
		t.Fatalf("failed path = %q", decoded.Failed[0].Path)
	}

	got, err := os.ReadFile(filepath.Join(dest, "ok.txt"))
	if err != nil || string(got) != "ok" {
		t.Fatalf("ok.txt = %q, err %v", got, err)
	}
	info, err := os.Stat(filepath.Join(dest, "blocked"))
	if err != nil || !info.IsDir() {
		t.Fatal("blocked directory was replaced")
	}
	waitTaskDone(t, task)
}

func TestUploadBatchResumesAcrossAttaches(t *testing.T) {
	dest := t.TempDir()
	req := apischema.FileUploadBatchRequest{
		Destination: dest,
		Files: []apischema.FileUploadBatchEntry{
			{Path: "resume.txt", Size: "10"},
		},
	}

	task := startUploadBatchTask(t, req)

	conn, _, attachErr := attachUploadBatchStream(t, task, "")
	writeData(t, conn, []byte("0123"))
	// Drop the connection without a close frame: the task parks in
	// waiting_for_client instead of failing.
	_ = conn.Close()
	select {
	case err := <-attachErr:
		if err != nil {
			t.Fatalf("first attach returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for first attach to detach")
	}

	// A stale offset must be rejected without disturbing the parked task.
	_, staleResults, _ := attachUploadBatchStream(t, task, "0")
	stale := waitResult(t, staleResults)
	if stale.Status != "error" || !strings.Contains(stale.Error, "offset mismatch") {
		t.Fatalf("stale attach result = %+v", stale)
	}

	conn2, results, _ := attachUploadBatchStream(t, task, "4")
	writeData(t, conn2, []byte("456789"))
	writeClose(t, conn2)

	result := waitResult(t, results)
	if result.Status != "ok" {
		t.Fatalf("resume result = %q (error %q)", result.Status, result.Error)
	}
	got, err := os.ReadFile(filepath.Join(dest, "resume.txt"))
	if err != nil || string(got) != "0123456789" {
		t.Fatalf("resume.txt = %q, err %v", got, err)
	}
	waitTaskDone(t, task)
}

func TestUploadBatchDoesNotOverwriteByDefault(t *testing.T) {
	dest := t.TempDir()
	if err := os.WriteFile(filepath.Join(dest, "exists.txt"), []byte("old"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	req := apischema.FileUploadBatchRequest{
		Destination: dest,
		Files: []apischema.FileUploadBatchEntry{
			{Path: "exists.txt", Size: "3"},
			{Path: "new.txt", Size: "3"},
		},
	}

	task := startUploadBatchTask(t, req)
	conn, results, _ := attachUploadBatchStream(t, task, "")
	writeData(t, conn, []byte("XXXnew"))
	writeClose(t, conn)

	result := waitResult(t, results)
	if result.Status != "ok" {
		t.Fatalf("result status = %q (error %q)", result.Status, result.Error)
	}
	decoded := decodeUploadBatchResult(t, result)
	if decoded.Succeeded != 1 || len(decoded.Failed) != 1 {
		t.Fatalf("result = %+v", decoded)
	}
	if decoded.Failed[0].Path != "exists.txt" || !strings.Contains(decoded.Failed[0].Error, "already exists") {
		t.Fatalf("failed item = %+v", decoded.Failed[0])
	}

	got, err := os.ReadFile(filepath.Join(dest, "exists.txt"))
	if err != nil || string(got) != "old" {
		t.Fatalf("existing file was touched: %q, err %v", got, err)
	}
	got, err = os.ReadFile(filepath.Join(dest, "new.txt"))
	if err != nil || string(got) != "new" {
		t.Fatalf("new.txt = %q, err %v", got, err)
	}
	waitTaskDone(t, task)
}

func TestUploadBatchOverwriteReplacesExisting(t *testing.T) {
	dest := t.TempDir()
	if err := os.WriteFile(filepath.Join(dest, "exists.txt"), []byte("old"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	overwrite := true
	req := apischema.FileUploadBatchRequest{
		Destination: dest,
		Files: []apischema.FileUploadBatchEntry{
			{Path: "exists.txt", Size: "3"},
		},
		Overwrite: &overwrite,
	}

	task := startUploadBatchTask(t, req)
	conn, results, _ := attachUploadBatchStream(t, task, "")
	writeData(t, conn, []byte("XXX"))
	writeClose(t, conn)

	result := waitResult(t, results)
	if result.Status != "ok" {
		t.Fatalf("result status = %q (error %q)", result.Status, result.Error)
	}
	decoded := decodeUploadBatchResult(t, result)
	if decoded.Succeeded != 1 || len(decoded.Failed) != 0 {
		t.Fatalf("result = %+v", decoded)
	}

	got, err := os.ReadFile(filepath.Join(dest, "exists.txt"))
	if err != nil || string(got) != "XXX" {
		t.Fatalf("exists.txt = %q, err %v", got, err)
	}
	waitTaskDone(t, task)
}

func TestUploadBatchRejectsExcessBytes(t *testing.T) {
	dest := t.TempDir()
	req := apischema.FileUploadBatchRequest{
		Destination: dest,
		Files: []apischema.FileUploadBatchEntry{
			{Path: "small.txt", Size: "3"},
		},
	}

	task := startUploadBatchTask(t, req)
	conn, results, _ := attachUploadBatchStream(t, task, "")
	writeData(t, conn, []byte("12345"))

	result := waitResult(t, results)
	if result.Status != "error" || !strings.Contains(result.Error, "size mismatch") {
		t.Fatalf("result = %+v", result)
	}

	snapshot := waitTaskDone(t, task)
	if snapshot.State != bridgetasks.TaskStateFailed {
		t.Fatalf("task state = %q, want failed", snapshot.State)
	}
}

func TestUploadBatchMissingDestinationFailsTask(t *testing.T) {
	req := apischema.FileUploadBatchRequest{
		Destination: filepath.Join(t.TempDir(), "does-not-exist"),
		Files: []apischema.FileUploadBatchEntry{
			{Path: "a.txt", Size: "1"},
		},
	}

	task := startUploadBatchTask(t, req)
	snapshot := waitTaskDone(t, task)
	if snapshot.State != bridgetasks.TaskStateFailed {
		t.Fatalf("task state = %q, want failed", snapshot.State)
	}
	if snapshot.Error == nil || snapshot.Error.Code != 404 {
		t.Fatalf("task error = %+v, want 404", snapshot.Error)
	}
}
