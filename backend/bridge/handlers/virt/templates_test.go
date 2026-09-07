package virt

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func templateTestImage(marker byte) []byte {
	image := make([]byte, 512)
	copy(image, "QFI\xfb")
	binary.BigEndian.PutUint32(image[4:8], 3)
	binary.BigEndian.PutUint32(image[100:104], 104)
	image[511] = marker
	return image
}

// HTTP, locking and publication use real implementations. These tests replace
// QEMU/XZ commands so they run without libvirt, root or optional host tools.
func fakeTemplateCommands(t *testing.T) *atomic.Int32 {
	t.Helper()
	oldLook, oldCommand := execLookPath, execCommand
	var decompresses atomic.Int32
	execLookPath = func(name string) (string, error) { return name, nil }
	execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		if name == "xz" {
			decompresses.Add(1)
			return exec.CommandContext(ctx, "cat", args[1])
		}
		if args[0] == "convert" {
			return exec.CommandContext(ctx, "cp", args[5], args[6])
		}
		return exec.CommandContext(ctx, "true")
	}
	t.Cleanup(func() { execLookPath, execCommand = oldLook, oldCommand })
	return &decompresses
}

func ensureTemplateForTest(ctx context.Context, store vmTemplateStore, preset vmImagePreset, id string, refresh bool) (apischema.VMTemplate, error) {
	var saved apischema.VMTemplate
	err := store.withPresetLock(ctx, preset.ID, func() error {
		var err error
		saved, err = store.ensure(ctx, preset, id, refresh, nil)
		return err
	})
	return saved, err
}

func TestTemplateReuseAcrossSessionsAndDeletionKeepsVMDisk(t *testing.T) {
	decompressions := fakeTemplateCommands(t)
	var downloads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		downloads.Add(1)
		_, _ = w.Write(templateTestImage(1))
	}))
	defer server.Close()
	store := vmTemplateStore{path: filepath.Join(t.TempDir(), "templates")}
	preset := vmImagePreset{ID: vmImagePresetHomeOS, Label: "Home Assistant OS", ImageURL: server.URL, ImageName: "haos.qcow2.xz", Version: "16.0"}
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			// A separate store value models another bridge sharing only disk state.
			if _, err := ensureTemplateForTest(t.Context(), vmTemplateStore{path: store.path}, preset, "", false); err != nil {
				t.Errorf("ensure: %v", err)
			}
		})
	}
	wg.Wait()
	if downloads.Load() != 1 || decompressions.Load() != 1 {
		t.Fatalf("downloads=%d decompressions=%d, want one each", downloads.Load(), decompressions.Load())
	}
	server.Close()
	saved, err := ensureTemplateForTest(t.Context(), store, preset, "", false)
	if err != nil {
		t.Fatalf("offline reuse: %v", err)
	}
	oldStore := templateStore
	templateStore = store
	t.Cleanup(func() { templateStore = oldStore })
	volume := filepath.Join(t.TempDir(), "vm.qcow2")
	if err = importImagePresetDiskFromCache(t.Context(), preset, saved.ID, volume, 32, nil); err != nil {
		t.Fatal(err)
	}
	// A VM disk must be writable without changing the base template.
	if err = os.WriteFile(volume, []byte("VM-specific data"), 0o600); err != nil {
		t.Fatal(err)
	}
	base, err := os.ReadFile(saved.Path)
	if err != nil || !bytes.Equal(base, templateTestImage(1)) {
		t.Fatalf("base changed: %v", err)
	}
	if err = store.delete(t.Context(), apischema.VMTemplateRequest{ImagePresetID: preset.ID, TemplateID: saved.ID}); err != nil {
		t.Fatal(err)
	}
	if data, readErr := os.ReadFile(volume); readErr != nil || string(data) != "VM-specific data" {
		t.Fatalf("VM lost after deletion: %v", readErr)
	}
	library, err := store.list(t.Context())
	if err != nil || len(library.Templates) != 0 {
		t.Fatalf("library after deletion: %+v, %v", library, err)
	}
	if _, err := ensureTemplateForTest(t.Context(), store, preset, saved.ID, false); errorCode(err, 0) != 404 {
		t.Fatalf("deleted pinned version: %v", err)
	}
}

func TestTemplateUpdatesPreserveVersionsAndUseHTTPValidators(t *testing.T) {
	fakeTemplateCommands(t)
	var version atomic.Int32
	version.Store(1)
	var downloads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		etag := fmt.Sprintf(`"build-%d"`, version.Load())
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		downloads.Add(1)
		w.Header().Set("ETag", etag)
		_, _ = w.Write(templateTestImage(byte(version.Load())))
	}))
	defer server.Close()
	store := vmTemplateStore{path: t.TempDir()}
	preset := vmImagePreset{ID: vmImagePresetDebian, Label: "Debian", ImageURL: server.URL, ImageName: "debian.qcow2", Version: "13"}
	first, err := ensureTemplateForTest(t.Context(), store, preset, "", false)
	if err != nil {
		t.Fatal(err)
	}
	same, err := ensureTemplateForTest(t.Context(), store, preset, "", true)
	if err != nil || same.ID != first.ID || downloads.Load() != 1 {
		t.Fatalf("unchanged update: %+v, %v, downloads=%d", same, err, downloads.Load())
	}
	version.Store(2)
	second, err := ensureTemplateForTest(t.Context(), store, preset, "", true)
	if err != nil || first.ID == second.ID {
		t.Fatalf("new version: %+v, %v", second, err)
	}
	library, err := store.list(t.Context())
	if err != nil || len(library.Templates) != 2 || library.Templates[0].ID != second.ID {
		t.Fatalf("versions: %+v, %v", library, err)
	}
	server.Close()
	pinned, err := ensureTemplateForTest(t.Context(), store, preset, first.ID, false)
	if err != nil || pinned.ID != first.ID {
		t.Fatalf("offline pin: %+v, %v", pinned, err)
	}
	newest, err := ensureTemplateForTest(t.Context(), store, preset, "", false)
	if err != nil || newest.ID != second.ID {
		t.Fatalf("default: %+v, %v", newest, err)
	}
}

func TestTemplateFailedDownloadDoesNotPublishAndRetrySucceeds(t *testing.T) {
	fakeTemplateCommands(t)
	var broken atomic.Bool
	broken.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if broken.Load() {
			w.Header().Set("Content-Length", "1024")
		}
		_, _ = w.Write(templateTestImage(1))
	}))
	defer server.Close()
	store := vmTemplateStore{path: t.TempDir()}
	preset := vmImagePreset{ID: vmImagePresetDebian, ImageURL: server.URL, ImageName: "debian.qcow2"}
	if _, err := ensureTemplateForTest(t.Context(), store, preset, "", false); err == nil {
		t.Fatal("truncated download accepted")
	}
	library, err := store.list(t.Context())
	if err != nil || len(library.Templates) != 0 {
		t.Fatalf("partial template published: %+v, %v", library, err)
	}
	if _, err := os.Stat(filepath.Join(store.path, string(preset.ID), ".download")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staging survived: %v", err)
	}
	broken.Store(false)
	if _, err := ensureTemplateForTest(t.Context(), store, preset, "", false); err != nil {
		t.Fatalf("retry: %v", err)
	}
}

func TestTemplateLockCancellationAndDeleteValidation(t *testing.T) {
	store := vmTemplateStore{path: t.TempDir()}
	id := fmt.Sprintf("%064x", 1)
	if err := store.withPresetLock(t.Context(), vmImagePresetDebian, func() error {
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		defer cancel()
		err := store.delete(ctx, apischema.VMTemplateRequest{ImagePresetID: vmImagePresetDebian, TemplateID: id})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("delete while copying: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, req := range []apischema.VMTemplateRequest{
		{ImagePresetID: vmImagePresetDebian, TemplateID: "../../vm.qcow2"},
		{ImagePresetID: "../../outside", TemplateID: id},
	} {
		if err := store.delete(t.Context(), req); errorCode(err, 0) != 400 {
			t.Fatalf("unsafe deletion accepted: %+v, %v", req, err)
		}
	}
	// A substituted directory cannot direct deletion outside the library.
	outside := t.TempDir()
	sentinel := filepath.Join(outside, "keep")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(store.path, string(vmImagePresetDebian))); err != nil {
		t.Fatal(err)
	}
	_ = store.delete(t.Context(), apischema.VMTemplateRequest{ImagePresetID: vmImagePresetDebian, TemplateID: id})
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("deleted outside store: %v", err)
	}
}

func TestTemplateRejectsUnsafeImagesBeforeQEMU(t *testing.T) {
	for _, kind := range []string{"invalid", "backing", "external"} {
		t.Run(kind, func(t *testing.T) {
			data := templateTestImage(1)
			switch kind {
			case "invalid":
				data[0] = 'X'
			case "backing":
				binary.BigEndian.PutUint64(data[8:16], 104)
			case "external":
				binary.BigEndian.PutUint64(data[72:80], 1<<2)
			}
			path := filepath.Join(t.TempDir(), "image.qcow2")
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			oldLook := execLookPath
			execLookPath = func(string) (string, error) { t.Fatal("QEMU invoked for unsafe image"); return "", nil }
			t.Cleanup(func() { execLookPath = oldLook })
			if err := validateTemplateDisk(t.Context(), path); err == nil {
				t.Fatal("unsafe image accepted")
			}
		})
	}
}

func TestTemplateCanceledDownloadDoesNotPublish(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = http.NewResponseController(w).Flush()
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	store := vmTemplateStore{path: t.TempDir()}
	preset := vmImagePreset{ID: vmImagePresetDebian, ImageURL: server.URL}
	result := make(chan error, 1)
	go func() { _, err := ensureTemplateForTest(ctx, store, preset, "", false); result <- err }()
	<-started
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("download cancellation: %v", err)
	}
	library, err := store.list(t.Context())
	if err != nil || len(library.Templates) != 0 {
		t.Fatalf("canceled download published: %+v, %v", library, err)
	}
}

func TestCachedHomeAssistantPreflightDoesNotRequireXZ(t *testing.T) {
	fakeTemplateCommands(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(templateTestImage(1)) }))
	defer server.Close()
	store := vmTemplateStore{path: t.TempDir()}
	preset := vmImagePreset{ID: vmImagePresetHomeOS, ImageURL: server.URL, ImageName: "haos.qcow2.xz"}
	if _, err := ensureTemplateForTest(t.Context(), store, preset, "", false); err != nil {
		t.Fatal(err)
	}
	oldStore := templateStore
	templateStore = store
	t.Cleanup(func() { templateStore = oldStore })
	execLookPath = func(name string) (string, error) {
		if name == "xz" {
			return "", os.ErrNotExist
		}
		return name, nil
	}
	preflight := apischema.VMPreflight{Firmware: apischema.VMPreflightFirmware{UEFIAvailable: true}}
	collectImagePresetPreflight(t.Context(), apischema.VMPreflightRequest{ImagePresetID: vmImagePresetHomeOS}, &preflight)
	if len(preflight.Errors) > 0 {
		t.Fatalf("cached image blocked: %v", preflight.Errors)
	}
}

func TestTemplateReleaseResolutionPreservesVersionAndReusesAsset(t *testing.T) {
	fakeTemplateCommands(t)
	var downloads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/latest" {
			_, _ = fmt.Fprintf(w, `{"tag_name":"16.0","assets":[{"name":"haos_ova-16.0.qcow2.xz","browser_download_url":"http://%s/assets/haos_ova-16.0.qcow2.xz"}]}`, r.Host)
			return
		}
		downloads.Add(1)
		_, _ = w.Write(templateTestImage(1))
	}))
	defer server.Close()
	preset := vmImagePresets[vmImagePresetHomeOS]
	preset.ReleaseAPIURL = server.URL + "/latest"
	preset.DownloadPrefix = server.URL + "/assets/"
	store := vmTemplateStore{path: t.TempDir()}
	first, err := ensureTemplateForTest(t.Context(), store, preset, "", true)
	if err != nil || first.Version != "16.0" {
		t.Fatalf("release metadata: %+v, %v", first, err)
	}
	second, err := ensureTemplateForTest(t.Context(), store, preset, "", true)
	if err != nil || first.ID != second.ID || downloads.Load() != 1 {
		t.Fatalf("repeat update: %+v, %v, downloads=%d", second, err, downloads.Load())
	}
}
