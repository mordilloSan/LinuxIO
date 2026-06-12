package watchtower

import (
	"reflect"
	"strings"
	"testing"
)

func TestRenderParseEnvRoundTrip(t *testing.T) {
	cfg := ScheduleConfig{
		Mode:           ModeCheckOnly,
		Time:           "05:30",
		Cleanup:        true,
		ContainerNames: []string{"app.service", "/redis", "app.service"},
	}
	data, err := RenderEnv(cfg)
	if err != nil {
		t.Fatalf("RenderEnv: %v", err)
	}
	got := ParseEnv(data)
	if got.Mode != ModeCheckOnly || !got.Cleanup {
		t.Fatalf("ParseEnv mode/cleanup = %+v", got)
	}
	if want := []string{"app.service", "redis"}; !reflect.DeepEqual(got.ContainerNames, want) {
		t.Fatalf("ContainerNames = %#v, want %#v", got.ContainerNames, want)
	}
	if !strings.Contains(string(data), `LINUXIO_WATCHTOWER_CONTAINERS=app\\.service redis`) {
		t.Fatalf("RenderEnv did not quote container names for systemd:\n%s", data)
	}
}

func TestRenderEnvEmptySelectionUsesSentinel(t *testing.T) {
	data, err := RenderEnv(DefaultScheduleConfig())
	if err != nil {
		t.Fatalf("RenderEnv: %v", err)
	}
	if !strings.Contains(string(data), "LINUXIO_WATCHTOWER_CONTAINERS="+NoContainersID+"\n") {
		t.Fatalf("RenderEnv empty selection:\n%s", data)
	}
}

func TestNormalizeScheduleConfigRejectsInvalidValues(t *testing.T) {
	if _, err := NormalizeScheduleConfig(ScheduleConfig{Mode: "daemon", Time: "04:00"}); err == nil {
		t.Fatal("NormalizeScheduleConfig accepted invalid mode")
	}
	if _, err := NormalizeScheduleConfig(ScheduleConfig{Mode: ModeUpdate, Time: "24:00"}); err == nil {
		t.Fatal("NormalizeScheduleConfig accepted invalid time")
	}
}

func TestRenderTimer(t *testing.T) {
	data, err := RenderTimer("06:45")
	if err != nil {
		t.Fatalf("RenderTimer: %v", err)
	}
	if !strings.Contains(string(data), "OnCalendar=*-*-* 06:45:00\n") {
		t.Fatalf("RenderTimer output:\n%s", data)
	}
	if got := ParseTimer(data); got != "06:45" {
		t.Fatalf("ParseTimer = %q, want 06:45", got)
	}
}

func TestParseContainerEnvValuePreservesMissingNames(t *testing.T) {
	got := ParseContainerEnvValue(`app\\.service missing redis`)
	want := []string{"app.service", "missing", "redis"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseContainerEnvValue = %#v, want %#v", got, want)
	}
}
