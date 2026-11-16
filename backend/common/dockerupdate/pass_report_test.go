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
			{Name: "docker-proxy", Project: "infra", Image: "example/proxy:latest", OldImageID: "sha256:aaaa", Err: errors.New("update Compose project \"infra\":\n pull failed")},
		},
	}
	var out strings.Builder
	report.Write(&out)
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("report = %q, want a totals line and one line per container", out.String())
	}
	if lines[0] != "Container updates: 14 checked, 1 updated, 1 failed" {
		t.Fatalf("totals = %q", lines[0])
	}
	if fields := strings.Join(strings.Fields(lines[1]), " "); fields != "updated nginx nginx:1.27 1.27.2 -> 1.27.3 (1a2b3c4d5e6f -> 4d5e6f708192)" {
		t.Fatalf("updated line = %q", lines[1])
	}
	// An error that carries Compose output stays on one journal line.
	if fields := strings.Join(strings.Fields(lines[2]), " "); fields != `failed docker-proxy (infra) update Compose project "infra": pull failed` {
		t.Fatalf("failed line = %q", lines[2])
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
}

func TestContainerOutcomeNamesImageProjectAndVersions(t *testing.T) {
	before := container.InspectResponse{
		Name:  "/web",
		Image: "sha256:1a2b3c4d5e6f0000",
		Config: &container.Config{
			Image: "nginx:1.27",
			// A standalone replacement inherits its labels from the container
			// it replaces, so the container's version label is not read.
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
	}
	for imageID, want := range map[string]string{"sha256:labelled": "2.4.1", "sha256:plain": "", "sha256:gone": "", "": ""} {
		if got := imageVersionOf(t.Context(), images, imageID); got != want {
			t.Errorf("imageVersionOf(%q) = %q, want %q", imageID, got, want)
		}
	}
}
