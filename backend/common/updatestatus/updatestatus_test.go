package updatestatus

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteReadRoundTripAndWireFormat(t *testing.T) {
	old := Path
	Path = filepath.Join(t.TempDir(), "update-status.json")
	t.Cleanup(func() { Path = old })

	started, finished, exit := int64(100), int64(200), 0
	rec := Record{Version: Version, ID: "id", OwnerUID: 1000, Status: StatusOK, ExitCode: &exit, StartedAt: &started, FinishedAt: &finished}
	if err := Write(rec); err != nil {
		t.Fatalf("Write: %v", err)
	}
	raw, err := os.ReadFile(Path)
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"version":1,"id":"id","owner_uid":1000,"status":"ok","exit_code":0,"started_at":100,"finished_at":200}` + "\n"
	if string(raw) != want {
		t.Fatalf("wire format = %q, want %q", raw, want)
	}
	got, err := Read()
	if err != nil || got == nil || !got.Valid() {
		t.Fatalf("Read = %+v, %v", got, err)
	}
}

func TestReadMissingEmptyAndInvalid(t *testing.T) {
	old := Path
	Path = filepath.Join(t.TempDir(), "update-status.json")
	t.Cleanup(func() { Path = old })

	if got, err := Read(); got != nil || err != nil {
		t.Fatalf("missing: %+v, %v", got, err)
	}
	if err := os.WriteFile(Path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := Read(); got != nil || err != nil {
		t.Fatalf("empty: %+v, %v", got, err)
	}
	if err := os.WriteFile(Path, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid: %v", err)
	}
}
