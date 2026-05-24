package indexer

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeTimerInterval(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "zero", raw: "0", want: "0"},
		{name: "zero duration", raw: "0s", want: "0"},
		{name: "trim and normalize", raw: " 30m ", want: "30m0s"},
		{name: "compound", raw: "1h30m", want: "1h30m0s"},
		{name: "empty", raw: " ", wantErr: true},
		{name: "negative", raw: "-1s", wantErr: true},
		{name: "invalid", raw: "soon", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeTimerInterval(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("normalizeTimerInterval: %v", err)
			}
			if got != tt.want {
				t.Fatalf("interval = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSetTimerIntervalUsesIndexerCLIAndReadsConfig(t *testing.T) {
	withTestIndexerCLI(t, func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("unexpected unconfigured command")
		return nil, nil
	})

	var calls [][]string
	indexerCLIOutput = func(_ context.Context, name string, args ...string) ([]byte, error) {
		call := append([]string{name}, args...)
		calls = append(calls, call)
		switch strings.Join(args, " ") {
		case "config set --interval 30m0s":
			return []byte("updated /etc/indexer/config.json\n"), nil
		case "config":
			return []byte(`{"index_path":"/","index_name":"root","interval":"30m0s"}`), nil
		default:
			t.Fatalf("unexpected args: %v", args)
			return nil, nil
		}
	}

	result, err := SetTimerInterval(context.Background(), "30m")
	if err != nil {
		t.Fatalf("SetTimerInterval: %v", err)
	}
	if result.Config.Interval != "30m0s" || result.Interval != "30m0s" {
		t.Fatalf("result = %#v", result)
	}
	if result.TimerUnit != indexerTimerUnitName {
		t.Fatalf("timer unit = %q, want %q", result.TimerUnit, indexerTimerUnitName)
	}
	wantCalls := [][]string{
		{"/usr/bin/indexer", "config", "set", "--interval", "30m0s"},
		{"/usr/bin/indexer", "config"},
	}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", calls, wantCalls)
	}
}

func TestSetTimerIntervalReportsCLIOutput(t *testing.T) {
	withTestIndexerCLI(t, func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte("systemctl failed"), errors.New("exit status 1")
	})

	_, err := SetTimerInterval(context.Background(), "5m")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "systemctl failed") {
		t.Fatalf("error = %q, want command output", err)
	}
}

func withTestIndexerCLI(t *testing.T, output func(context.Context, string, ...string) ([]byte, error)) {
	t.Helper()
	origLookPath := indexerCLILookPath
	origStat := indexerCLIStat
	origOutput := indexerCLIOutput
	indexerCLILookPath = func(string) (string, error) {
		return "/usr/bin/indexer", nil
	}
	indexerCLIStat = origStat
	indexerCLIOutput = output
	t.Cleanup(func() {
		indexerCLILookPath = origLookPath
		indexerCLIStat = origStat
		indexerCLIOutput = origOutput
	})
}
