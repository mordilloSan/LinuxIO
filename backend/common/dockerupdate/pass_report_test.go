package dockerupdate

import (
	"context"
	"encoding/json/v2"
	"errors"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"github.com/mordilloSan/LinuxIO/backend/common/scheduledrun"
)

func TestPassReportSummary(t *testing.T) {
	updated := ContainerOutcome{Name: "web", NewImageID: "sha256:new"}
	failed := ContainerOutcome{Name: "proxy", Err: errors.New("container keeps restarting")}
	rolledBack := ContainerOutcome{Name: "app", Err: rolledBackError{err: errors.New("container health is unhealthy"), to: "image old"}}
	tests := map[string]struct {
		report PassReport
		want   scheduledrun.Summary
		none   bool
	}{
		"ended before the check": {report: PassReport{}, none: true},
		"skipped": {
			report: PassReport{Skipped: passSkippedBusy},
			want:   scheduledrun.Summary{Message: "Skipped: another Docker update was running", Skipped: true},
		},
		"nothing to update": {
			report: PassReport{Mode: "update", Checked: DockerUpdateCheckResult{Checked: 4}},
			want:   scheduledrun.Summary{Message: "Nothing to update"},
		},
		"updated and failed": {
			report: PassReport{Mode: "update", Checked: DockerUpdateCheckResult{Checked: 4, Updates: 2}, Containers: []ContainerOutcome{updated, failed}},
			want:   scheduledrun.Summary{Message: "1 updated, 1 failed", Failures: 1},
		},
		// A rolled back update failed, with an alert of its own.
		"rolled back": {
			report: PassReport{Mode: "update", Checked: DockerUpdateCheckResult{Checked: 4, Updates: 3}, Containers: []ContainerOutcome{updated, failed, rolledBack}},
			want:   scheduledrun.Summary{Message: "1 updated, 1 failed, 1 rolled back", Failures: 2},
		},
		// Check-only leaves every update waiting, and an updating pass those
		// of the containers it is not set to update.
		"check only": {
			report: PassReport{Mode: "check_only", Checked: DockerUpdateCheckResult{Checked: 4, Updates: 2, Errors: 1}},
			want:   scheduledrun.Summary{Message: "1 could not be checked, 2 waiting"},
		},
		// A pass that touched a container does not say what still waits: a
		// failed container may or may not be one the check counted.
		"updated with others waiting": {
			report: PassReport{Mode: "update", Checked: DockerUpdateCheckResult{Checked: 4, Updates: 3}, Containers: []ContainerOutcome{updated}},
			want:   scheduledrun.Summary{Message: "1 updated"},
		},
		// Too little free space: nothing was updated, so what waits is named.
		// The run fails on its error; the summary counts no failed container.
		"low free space": {
			report: PassReport{Mode: "update", Checked: DockerUpdateCheckResult{Checked: 4, Updates: 2}, LowFreeSpace: true},
			want:   scheduledrun.Summary{Message: "Updates skipped for low free space, 2 waiting"},
		},
		// The check failed, then the pass found too little room to update.
		"failed check, low free space": {
			report: PassReport{LowFreeSpace: true},
			want:   scheduledrun.Summary{Message: "Updates skipped for low free space"},
		},
		"low free space, nothing waiting": {
			report: PassReport{Mode: "update", Checked: DockerUpdateCheckResult{Checked: 4}, LowFreeSpace: true},
			want:   scheduledrun.Summary{Message: "Updates skipped for low free space"},
		},
		// The check failed, so the pass has no mode, but it reached a container.
		"failed check, one container failed": {
			report: PassReport{Containers: []ContainerOutcome{failed}},
			want:   scheduledrun.Summary{Message: "1 failed", Failures: 1},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, ok := tc.report.Summary()
			if ok == tc.none || got != tc.want {
				t.Fatalf("Summary = %+v, %v; want %+v, %v", got, ok, tc.want, !tc.none)
			}
		})
	}
}

func TestPassReportWriteNamesEachContainer(t *testing.T) {
	report := PassReport{
		Mode:    "update",
		Checked: DockerUpdateCheckResult{Checked: 14, Updates: 2},
		Containers: []ContainerOutcome{
			{Name: "nginx", Image: "nginx:1.27", OldImageID: "sha256:1a2b3c4d5e6f0000", NewImageID: "sha256:4d5e6f7081920000", OldVersion: "1.27.2", NewVersion: "1.27.3"},
			{Name: "docker-proxy", Project: "infra", Image: "example/proxy:latest", OldImageID: "sha256:aaaa", Err: errors.New("update Compose project \"infra\":\n pull\x1b failed\u2066")},
			{Name: "app", Project: "infra", Image: "example/app:1", OldImageID: "sha256:bbbb", Err: rolledBackError{err: errors.New("container health is \"unhealthy\""), to: "image bbbb"}},
		},
	}
	var out strings.Builder
	report.Write(&out)
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("report = %q, want a totals line and one line per container", out.String())
	}
	if lines[0] != "Container updates: 14 checked, 1 updated, 1 failed, 1 rolled back" {
		t.Fatalf("totals = %q", lines[0])
	}
	if fields := strings.Join(strings.Fields(lines[1]), " "); fields != "updated nginx nginx:1.27 1.27.2 -> 1.27.3 (1a2b3c4d5e6f -> 4d5e6f708192)" {
		t.Fatalf("updated line = %q", lines[1])
	}
	// An error that carries Compose output stays on one journal line, without
	// the control and bidirectional characters others wrote into it.
	if fields := strings.Join(strings.Fields(lines[2]), " "); fields != `failed docker-proxy (infra) update Compose project "infra": pull failed` {
		t.Fatalf("failed line = %q", lines[2])
	}
	if fields := strings.Join(strings.Fields(lines[3]), " "); fields != `rolled back app (infra) container health is "unhealthy"; rolled back to image bbbb` {
		t.Fatalf("rolled back line = %q", lines[3])
	}

	out.Reset()
	PassReport{}.Write(&out)
	if out.Len() != 0 {
		t.Fatalf("a pass that checked nothing printed %q", out.String())
	}
	PassReport{Skipped: passSkippedBusy}.Write(&out)
	if out.String() != "Container updates: skipped, another Docker update was running\n" {
		t.Fatalf("skipped report = %q", out.String())
	}
	out.Reset()
	PassReport{Mode: "update", Checked: DockerUpdateCheckResult{Checked: 1}, LowFreeSpace: true}.Write(&out)
	if out.String() != "Container updates: 1 checked, updates skipped for low free space\n" {
		t.Fatalf("low free space report = %q", out.String())
	}
}

func TestContainerOutcomeNamesImageProjectAndVersions(t *testing.T) {
	before := container.InspectResponse{
		Name:  "/web",
		Image: "sha256:1a2b3c4d5e6f0000",
		Config: &container.Config{
			Image: "nginx:1.27",
			// A container's labels can name another version than its
			// image's, so the container's version label is not read.
			Labels: map[string]string{
				imageVersionLabel:            "0.9.0",
				"com.docker.compose.project": "site",
			},
		},
	}
	outcome := outcomeBefore(before)
	if outcome.Name != "web" || outcome.Project != "site" || outcome.Image != "nginx:1.27" || outcome.OldVersion != "" {
		t.Fatalf("outcome = %+v, want the name, project and image of the container and no version", outcome)
	}
	outcome.NewImageID, outcome.OldVersion, outcome.NewVersion = "sha256:4d5e6f7081920000", "1.27.2", "1.27.3"
	if got, want := outcome.sentence(), "Image nginx:1.27 went from version 1.27.2 to 1.27.3 (1a2b3c4d5e6f to 4d5e6f708192)."; got != want {
		t.Fatalf("sentence = %q, want %q", got, want)
	}
	// An image without a version label, or whose version did not change (a
	// rebuilt tag), is named by its IDs alone.
	outcome.NewVersion = "1.27.2"
	if got, want := outcome.sentence(), "Image nginx:1.27 was replaced (1a2b3c4d5e6f to 4d5e6f708192)."; got != want {
		t.Fatalf("sentence = %q, want %q", got, want)
	}
	if got, want := outcome.change(), "nginx:1.27 (1a2b3c4d5e6f -> 4d5e6f708192)"; got != want {
		t.Fatalf("change = %q, want %q", got, want)
	}
	if got, want := (ContainerOutcome{OldImageID: "sha256:old", NewImageID: "sha256:new"}).sentence(), "Image old was replaced by new."; got != want {
		t.Fatalf("sentence = %q, want %q", got, want)
	}
}

type fakeImageInspector map[string]map[string]string

func (f fakeImageInspector) ImageInspect(_ context.Context, imageID string, _ ...client.ImageInspectOption) (client.ImageInspectResult, error) {
	labels, ok := f[imageID]
	if !ok {
		return client.ImageInspectResult{}, errors.New("no such image")
	}
	// Decoded as the engine's answer is, which also keeps the image
	// specification module out of this module's direct requirements.
	encoded, err := json.Marshal(map[string]any{"Config": map[string]any{"Labels": labels}})
	if err != nil {
		return client.ImageInspectResult{}, err
	}
	var result client.ImageInspectResult
	err = json.Unmarshal(encoded, &result)
	return result, err
}

func TestImageVersionOfReadsTheImagesLabel(t *testing.T) {
	images := fakeImageInspector{
		"sha256:labelled": {imageVersionLabel: "2.4.1"},
		"sha256:plain":    {},
		// The image's author writes the label: an escape sequence, a line
		// break and a bidirectional override are dropped or joined.
		"sha256:hostile": {imageVersionLabel: "1.0\x1b]0;owned\x07\nupdated: all\u202e"},
		"sha256:long":    {imageVersionLabel: strings.Repeat("9", 100)},
	}
	for imageID, want := range map[string]string{
		"sha256:labelled": "2.4.1",
		"sha256:plain":    "",
		"sha256:gone":     "",
		"":                "",
		"sha256:hostile":  "1.0]0;owned updated: all",
		"sha256:long":     strings.Repeat("9", maxVersionRunes-1) + "…",
	} {
		if got := imageVersionOf(t.Context(), images, imageID); got != want {
			t.Errorf("imageVersionOf(%q) = %q, want %q", imageID, got, want)
		}
	}
}
