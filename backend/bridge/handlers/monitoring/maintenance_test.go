package monitoring

import (
	"context"
	"net/http"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func TestMonitoringMaintenanceCommandsUseControlClient(t *testing.T) {
	tests := []struct {
		name    string
		command string
		data    string
		check   func(t *testing.T, got any)
	}{
		{
			name:    "refresh smart",
			command: "smart.refresh",
			data:    `{"refreshed":true}`,
			check: func(t *testing.T, got any) {
				result, ok := got.(apischema.MonitoringSmartRefreshResult)
				if !ok || !result.Refreshed {
					t.Fatal("refresh result = false, want true")
				}
			},
		},
		{
			name:    "check database",
			command: "db.check",
			data:    `{"path":"/var/lib/linuxio/monitoring/metrics.db"}`,
			check: func(t *testing.T, got any) {
				result, ok := got.(apischema.MonitoringDatabaseResult)
				if !ok || result.Path == "" {
					t.Fatal("database result has no path")
				}
			},
		},
		{
			name:    "maintain database",
			command: "db.maintain",
			data:    `{"path":"/var/lib/linuxio/monitoring/metrics.db"}`,
			check: func(t *testing.T, got any) {
				result, ok := got.(apischema.MonitoringDatabaseResult)
				if !ok || result.Path == "" {
					t.Fatal("database result has no path")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withTestControlClient(t, func(req *http.Request) (*http.Response, error) {
				command := decodeCommandRequest(t, req)
				if command.Command != tc.command {
					t.Fatalf("command = %q, want %q", command.Command, tc.command)
				}
				return jsonResponse(http.StatusOK, `{"ok":true,"command":"`+tc.command+`","data":`+tc.data+`}`), nil
			})

			var got any
			var err error
			switch tc.command {
			case "smart.refresh":
				got, err = RefreshSmart(context.Background())
			case "db.check":
				got, err = CheckDatabase(context.Background())
			case "db.maintain":
				got, err = MaintainDatabase(context.Background())
			}
			if err != nil {
				t.Fatalf("command error = %v", err)
			}
			tc.check(t, got)
		})
	}
}
