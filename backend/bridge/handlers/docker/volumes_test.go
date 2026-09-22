package docker

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/client"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

type volumeListTransport func(*http.Request) (*http.Response, error)

func (f volumeListTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type volumeUsageTestCase struct {
	name, version, usage string
	usageError           error
	cancel               bool
	wantSize             int64
}

func TestListVolumesUsage(t *testing.T) {
	for _, test := range []volumeUsageTestCase{
		{name: "current API", version: "1.55", usage: `{"VolumeUsage":{"Items":[{"Name":"data","UsageData":{"Size":2048,"RefCount":3}},{"Name":"empty","UsageData":{"Size":0,"RefCount":0}}]}}`, wantSize: 2048},
		{name: "legacy API", version: "1.51", usage: `{"Volumes":[{"Name":"data","UsageData":{"Size":2048,"RefCount":3}},{"Name":"empty","UsageData":{"Size":0,"RefCount":0}}]}`, wantSize: 2048},
		{name: "unsupported usage", version: "1.55", usageError: errors.New("usage unsupported"), wantSize: -1},
		{name: "usage timeout", version: "1.55", usageError: context.DeadlineExceeded, wantSize: -1},
		{name: "missing usage", version: "1.55", usage: `{}`, wantSize: -1},
		{name: "caller cancellation", version: "1.55", cancel: true, usageError: context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cli := newVolumeUsageTestClient(t, test, cancel)
			volumes, err := listVolumes(ctx, cli)
			if test.cancel {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("error = %v, want caller cancellation", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			assertVolumeUsage(t, volumes, test.wantSize)
		})
	}
}

func newVolumeUsageTestClient(t *testing.T, test volumeUsageTestCase, cancel context.CancelFunc) *client.Client {
	t.Helper()
	cli, err := client.New(client.WithAPIVersion(test.version), client.WithHTTPClient(&http.Client{
		Transport: volumeListTransport(func(r *http.Request) (*http.Response, error) {
			body := ""
			switch strings.TrimPrefix(r.URL.Path, "/v"+test.version) {
			case "/volumes":
				body = `{"Volumes":[{"Name":"missing","Driver":"plugin"},{"Name":"empty","Driver":"local"},{"Name":"data","Driver":"local","Mountpoint":"/var/lib/docker/volumes/data/_data","Labels":{"purpose":"test"}}]}`
			case "/containers/json":
				if r.URL.Query().Get("all") != "1" {
					t.Error("reference count must include stopped containers")
				}
				body = `[{"Id":"running","Names":["/writer"],"State":"running","Mounts":[{"Name":"data","Type":"volume"},{"Name":"data","Type":"volume"}]},{"Id":"stopped","Names":["/reader"],"State":"exited","Mounts":[{"Name":"data","Type":"volume"}]}]`
			case "/system/df":
				if r.URL.Query().Get("type") != "volume" || len(r.URL.Query()["type"]) != 1 || r.URL.Query().Get("verbose") != "1" {
					t.Errorf("disk usage query = %s, want only verbose volume usage", r.URL.RawQuery)
				}
				if deadline, ok := r.Context().Deadline(); !ok || time.Until(deadline) > 5*time.Second {
					t.Error("volume usage must have a bounded deadline")
				}
				if test.cancel {
					cancel()
				}
				if test.usageError != nil {
					return nil, test.usageError
				}
				body = test.usage
			default:
				t.Errorf("unexpected Docker request: %s", r.URL.Path)
			}
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
		}),
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	return cli
}

func assertVolumeUsage(t *testing.T, volumes []apischema.DockerVolume, wantSize int64) {
	t.Helper()
	if len(volumes) != 3 {
		t.Fatalf("volumes = %#v, want three volumes", volumes)
	}
	data := volumes[0]
	wantReferences := int64(2)
	if wantSize >= 0 {
		wantReferences = 3 // Preserve Docker's count when supplied.
	}
	if data.Name != "data" || data.Mountpoint != "/var/lib/docker/volumes/data/_data" || data.Labels["purpose"] != "test" || len(data.Containers) != 2 || data.UsageData == nil || data.UsageData.Size != wantSize || data.UsageData.RefCount != wantReferences {
		t.Fatalf("data volume = %#v, usage = %#v", data, data.UsageData)
	}
	if empty := volumes[1].UsageData; empty == nil || empty.RefCount != 0 || (wantSize >= 0 && empty.Size != 0) {
		t.Fatalf("empty volume usage = %#v", empty)
	}
	if missing := volumes[2].UsageData; missing == nil || missing.Size != -1 || missing.RefCount != 0 {
		t.Fatalf("missing volume usage = %#v, want unknown size and zero references", missing)
	}
}
