package appupdate

import (
	"encoding/json/v2"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"

	ver "github.com/mordilloSan/LinuxIO/backend/common/version"
)

// stubComponentVersions makes this build report version and the installer's
// record hold recorded (or fail to be read with err).
func stubComponentVersions(t *testing.T, version string, recorded map[string]string, err error) {
	t.Helper()
	oldVersion, oldRead := ver.Version, readComponentVersions
	t.Cleanup(func() { ver.Version, readComponentVersions = oldVersion, oldRead })
	ver.Version = version
	readComponentVersions = func() (map[string]string, error) {
		return maps.Clone(recorded), err
	}
}

func versionResponse(t *testing.T) map[string]string {
	t.Helper()
	rec := httptest.NewRecorder()
	handleVersion(rec, httptest.NewRequest(http.MethodGet, "/api/version", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	return got
}

// The handler answers with what the installer recorded, and with this build's
// own version for the webserver whatever the record says of it.
func TestVersionHandlerReturnsRecordedVersionsAndItsOwn(t *testing.T) {
	stubComponentVersions(t, "v9.9.9", map[string]string{
		"LinuxIO CLI":        "v4.5.6",
		"LinuxIO Web Server": "v0.0.1",
		"LinuxIO Bridge":     "v2.3.4",
		"LinuxIO Auth":       "v3.4.5",
		"LinuxIO Indexer":    "v5.6.7",
		"LinuxIO Monitoring": "v6.7.8",
	}, nil)

	assertVersionMap(t, versionResponse(t), map[string]string{
		"LinuxIO CLI":        "v4.5.6",
		"LinuxIO Web Server": "v9.9.9",
		"LinuxIO Bridge":     "v2.3.4",
		"LinuxIO Auth":       "v3.4.5",
		"LinuxIO Indexer":    "v5.6.7",
		"LinuxIO Monitoring": "v6.7.8",
	})
}

// A program the installer could not run is left out, as a failed probe was.
func TestVersionHandlerLeavesOutWhatWasNotRecorded(t *testing.T) {
	stubComponentVersions(t, "v9.9.9", map[string]string{"LinuxIO Bridge": "v2.3.4"}, nil)

	assertVersionMap(t, versionResponse(t), map[string]string{
		"LinuxIO Web Server": "v9.9.9",
		"LinuxIO Bridge":     "v2.3.4",
	})
}

// Without the record, such as a build run in place, the webserver still
// answers with its own version: the page waits on this endpoint after an
// update.
func TestVersionHandlerAnswersWithoutTheRecord(t *testing.T) {
	stubComponentVersions(t, "v9.9.9", nil, errors.New("no such file"))

	assertVersionMap(t, versionResponse(t), map[string]string{"LinuxIO Web Server": "v9.9.9"})
}

func TestWebserverComponentIsInTheComponentList(t *testing.T) {
	for _, c := range ver.Components {
		if c.Name == webserverComponent && c.Binary == "linuxio-webserver" {
			return
		}
	}
	t.Fatalf("ver.Components has no %q", webserverComponent)
}

func assertVersionMap(t *testing.T, got, want map[string]string) {
	t.Helper()
	if !maps.Equal(got, want) {
		t.Fatalf("versions = %#v, want %#v", got, want)
	}
}
