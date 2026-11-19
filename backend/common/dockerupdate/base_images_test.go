package dockerupdate

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
	digest "github.com/opencontainers/go-digest"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
)

// The digests the registry gave for the measured bases, which their FROM
// lines carry.
var (
	alpineBase  = baseImage{Ref: "docker.io/library/alpine:3.20", Digest: "sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc"}
	busyboxBase = baseImage{Ref: "docker.io/library/busybox:1.36", Digest: "sha256:73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662"}
)

// collectRecord reads output as the rebuild does: each line, trimmed, through
// its emitter.
func collectRecord(t *testing.T, output string) builtImageRecord {
	t.Helper()
	var bases buildBaseCollector
	if err := readComposeLines(strings.NewReader(output), composeLineLimit, func(line string) {
		if line = strings.TrimSpace(line); line != "" {
			bases.add(line)
		}
	}); err != nil {
		t.Fatal(err)
	}
	return bases.record("sha256:built", time.Now())
}

// collectBases is the bases collectRecord finds, failing the test when it
// could not read them.
func collectBases(t *testing.T, output string) []baseImage {
	t.Helper()
	record := collectRecord(t, output)
	if record.BasesUnread {
		t.Fatalf("the build's output was not understood: %+v", record)
	}
	return record.Bases
}

func TestBuildBaseCollectorReadsTheMeasuredBuildOutput(t *testing.T) {
	for setup, tc := range map[string]struct {
		outputs map[string]string
		want    map[string][]baseImage
	}{
		"Compose's builder": {measuredBuildOutputs, map[string][]baseImage{
			"first build":          {alpineBase},
			"fully cached rebuild": {alpineBase},
			// Two registry bases, each once though BuildKit writes a line
			// again while it pulls; the stage built FROM an earlier stage has
			// none.
			"multi-stage":         {busyboxBase, alpineBase},
			"multi-stage, cached": {alpineBase, busyboxBase},
			"FROM scratch":        {},
			// A base the Dockerfile pins by digest can never move.
			"pinned by digest":       {},
			"pinned by digest alone": {},
			// The Dockerfile frontend the syntax line names is not a base.
			"syntax line": {alpineBase},
			"FROM an ARG": {alpineBase},
		}},
		// Through Bake the bracket names no service: "[1/2]", "[builder 1/2]".
		"buildx and Bake": {measuredBakeBuildOutputs, map[string][]baseImage{
			"first build":            {alpineBase},
			"fully cached rebuild":   {alpineBase},
			"multi-stage":            {alpineBase, busyboxBase},
			"multi-stage, cached":    {busyboxBase, alpineBase},
			"FROM scratch":           {},
			"pinned by digest":       {},
			"pinned by digest alone": {},
		}},
	} {
		if len(tc.want) != len(tc.outputs) {
			t.Fatalf("%s: %d cases for %d measured outputs", setup, len(tc.want), len(tc.outputs))
		}
		for name, output := range tc.outputs {
			t.Run(setup+", "+name, func(t *testing.T) {
				if got := collectBases(t, output); !slices.Equal(got, tc.want[name]) {
					t.Fatalf("bases = %+v, want %+v", got, tc.want[name])
				}
			})
		}
	}
}

// A step's output is written behind "#<n> <seconds> ", so nothing a step
// prints, nor the text of a step itself, reads as a base or as a pin.
func TestBuildBaseCollectorIgnoresWhatAStepWrites(t *testing.T) {
	zeros := "sha256:" + strings.Repeat("0", 64)
	for setup, real := range map[string][]string{
		"Compose's builder": {
			"#2 [app internal] load metadata for docker.io/library/alpine:3.20",
			"#4 [app 1/2] FROM " + alpineBase.Ref + "@" + alpineBase.Digest,
		},
		"buildx and Bake": {
			"#3 [internal] load metadata for docker.io/library/alpine:3.20",
			"#5 [1/2] FROM " + alpineBase.Ref + "@" + alpineBase.Digest,
		},
	} {
		t.Run(setup, func(t *testing.T) {
			output := strings.Join(append(real,
				// A step pinning the real base, which would hide it.
				"#5 0.183 #2 [app internal] load metadata for "+alpineBase.Ref+"@"+alpineBase.Digest,
				"#6 0.165 #2 [internal] load metadata for "+alpineBase.Ref+"@"+alpineBase.Digest,
				"#5 [app 2/2] RUN echo ] FROM docker.io/library/evil:1@"+zeros,
				"#6 [2/2] RUN echo ] FROM docker.io/library/evil:1@"+zeros,
				"#5 0.183 #9 0.123 FROM docker.io/library/evil:2@"+zeros,
				"#5 0.183 \r#5 [1/2] FROM docker.io/library/evil:3@"+zeros,
				"#5 0.183 #5 [app 1/2] FROM docker.io/library/evil:4@"+zeros,
				"#6 0.165 #6 [1/2] FROM docker.io/library/evil:4@"+zeros,
				"#6 [1/2] FROM docker.io/library/evil:5@"+zeros+" trailing",
				"#6 [] FROM docker.io/library/evil:5@"+zeros,
				// Not a reference, or no valid digest.
				"#6 [app 1/2] FROM Docker.io/Evil:6@"+zeros,
				"#6 [1/2] FROM docker.io/library/evil:7@sha256:short",
				"#6 [1/2] FROM docker.io/library/evil:8",
			), "\n")
			if got := collectBases(t, output); !slices.Equal(got, []baseImage{alpineBase}) {
				t.Fatalf("bases = %+v, want only the base the build resolved", got)
			}
		})
	}
}

func TestBuildBaseCollectorKeepsAtMostMaxBases(t *testing.T) {
	var lines []string
	for i := range maxBuiltImageBases + 8 {
		lines = append(lines, fmt.Sprintf("#%d [stage%d 1/1] FROM docker.io/library/base%d:1@%s", i, i, i, digest.FromString(fmt.Sprint(i))))
	}
	if got := collectBases(t, strings.Join(lines, "\n")); len(got) != maxBuiltImageBases || got[0].Ref != "docker.io/library/base0:1" {
		t.Fatalf("kept %d bases, want the first %d", len(got), maxBuiltImageBases)
	}
}

// A build that looked a tag up and wrote no base line this reader knows was
// not understood: its record says so instead of claiming the image has no
// base. One that looked no tag up (FROM scratch, bases pinned by digest) has
// no base that can move.
func TestBuildBaseCollectorTellsAnUnreadBuildFromOneWithoutBases(t *testing.T) {
	for name, output := range map[string]string{
		"another form": strings.ReplaceAll(measuredBakeBuildOutputs["first build"], "] FROM ", "] BASE "),
		"multi-stage":  strings.ReplaceAll(measuredBuildOutputs["multi-stage"], "] FROM ", "] from "),
		"no bracket":   strings.ReplaceAll(measuredBakeBuildOutputs["fully cached rebuild"], "[1/2] FROM", "FROM"),
	} {
		t.Run(name, func(t *testing.T) {
			if record := collectRecord(t, output); !record.BasesUnread || len(record.Bases) != 0 {
				t.Fatalf("record = %+v, want the bases unread", record)
			}
		})
	}
	for _, name := range []string{"FROM scratch", "pinned by digest", "pinned by digest alone"} {
		if record := collectRecord(t, measuredBakeBuildOutputs[name]); record.BasesUnread {
			t.Fatalf("%s: record = %+v, want no base, read", name, record)
		}
	}
}

// The records live beside the statuses: every writer of the statuses keeps
// them, a single-container check included, and only the check of every
// container drops those of images no longer on the host, unless they were
// written after the image list was asked for.
func TestBuiltImageRecordsSurviveStatusWritesAndArePruned(t *testing.T) {
	withTempUpdateStatusPath(t)
	ctx := t.Context()
	listedAt := time.Now()
	before := listedAt.Add(-time.Minute)
	for _, record := range []builtImageRecord{
		{ImageID: "sha256:running", Bases: []baseImage{alpineBase}, RecordedAt: before},
		{ImageID: "sha256:down", Bases: []baseImage{busyboxBase}, RecordedAt: before},
		{ImageID: "sha256:gone", RecordedAt: before},
		{ImageID: "sha256:just-built", RecordedAt: listedAt.Add(time.Second)},
	} {
		if err := recordBuiltImage(ctx, record); err != nil {
			t.Fatalf("recordBuiltImage: %v", err)
		}
	}
	// A second record of an image replaces the first.
	moved := baseImage{Ref: alpineBase.Ref, Digest: digest.FromString("older").String()}
	if err := recordBuiltImage(ctx, builtImageRecord{ImageID: "sha256:running", Bases: []baseImage{moved}, RecordedAt: before}); err != nil {
		t.Fatal(err)
	}
	if err := mergeUpdateStatuses(ctx, []ImageUpdateStatus{{ContainerName: "web", CheckState: DockerUpdateCheckStateBuilt}}); err != nil {
		t.Fatal(err)
	}
	if err := writeUpdateStatuses(ctx, []ImageUpdateStatus{{ContainerName: "web", CheckState: DockerUpdateCheckStateBuilt}}, nil); err != nil {
		t.Fatal(err)
	}
	if records := readBuiltImageRecords(); len(records) != 4 || !slices.Equal(records["sha256:running"].Bases, []baseImage{moved}) {
		t.Fatalf("records = %+v, want all four kept, the running one replaced", records)
	}

	// The image of a project that is down is still on the host.
	images := hostImages{ids: map[string]bool{"sha256:running": true, "sha256:down": true}, listedAt: listedAt}
	if err := writeUpdateStatuses(ctx, nil, &images); err != nil {
		t.Fatal(err)
	}
	records := readBuiltImageRecords()
	if _, ok := records["sha256:gone"]; ok || len(records) != 3 {
		t.Fatalf("records = %+v, want the one of the image gone from the host dropped", records)
	}
	if _, ok := records["sha256:just-built"]; !ok {
		t.Fatal("a record written after the image list was dropped")
	}
}

// A status file written before the records existed reads as it did.
func TestStatusFileWithoutBuiltImagesReadsFine(t *testing.T) {
	path := withTempUpdateStatusPath(t)
	content := `{"version": 2, "statuses": [{"container_name": "web", "check_state": "built", "update_available": false, "checked_at": "2026-11-01T00:00:00Z"}]}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	if status, ok := ReadStatusSnapshot().ForContainerName("web"); !ok || status.CheckState != DockerUpdateCheckStateBuilt {
		t.Fatalf("status = %+v, %v", status, ok)
	}
	if records := readBuiltImageRecords(); len(records) != 0 {
		t.Fatalf("records = %+v, want none", records)
	}
	if err := recordBuiltImage(t.Context(), builtImageRecord{ImageID: "sha256:web", RecordedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadStatusSnapshot().ForContainerName("web"); !ok {
		t.Fatal("recording an image dropped the statuses")
	}
}

// builtRegistry answers for images that have no repository digest, as the
// classic image store gives a built one, and for the current digests of
// alpine and busybox; gone:1 is refused.
func builtRegistry(imageIDs ...string) *fakeImageUpdateCheckClient {
	images := make(map[string]client.ImageInspectResult, len(imageIDs))
	for _, id := range imageIDs {
		images[id] = client.ImageInspectResult{ID: id}
	}
	return &fakeImageUpdateCheckClient{
		images: images,
		distributions: map[string]client.DistributionInspectResult{
			alpineBase.Ref:  distributionInspectResult(digest.Digest(alpineBase.Digest)),
			busyboxBase.Ref: distributionInspectResult(digest.Digest(busyboxBase.Digest)),
		},
		distributionErrors: map[string]error{
			"docker.io/library/gone:1": errors.New("Error response from daemon: manifest unknown"),
		},
	}
}

func TestCheckComparesTheBaseImagesOfBuiltImages(t *testing.T) {
	withTempUpdateStatusPath(t)
	ctx := t.Context()
	older := baseImage{Ref: alpineBase.Ref, Digest: digest.FromString("older").String()}
	for _, record := range []builtImageRecord{
		{ImageID: "same-id", Bases: []baseImage{alpineBase, busyboxBase}},
		{ImageID: "moved-id", Bases: []baseImage{older, busyboxBase}},
		{ImageID: "failed-id", Bases: []baseImage{alpineBase, {Ref: "docker.io/library/gone:1", Digest: busyboxBase.Digest}}},
		{ImageID: "scratch-id"},
		{ImageID: "unread-id", BasesUnread: true},
	} {
		record.RecordedAt = time.Now()
		if err := recordBuiltImage(ctx, record); err != nil {
			t.Fatal(err)
		}
	}
	registry := builtRegistry("norecord-id", "same-id", "moved-id", "failed-id", "scratch-id", "unread-id")
	var targets []containerImageUpdateTarget
	reasons := map[string]string{}
	for _, name := range []string{"norecord", "same", "moved", "failed", "scratch", "unread"} {
		targets = append(targets, containerImageUpdateTarget{ContainerID: name, ContainerName: name, ImageID: name + "-id", ImageRef: "media-" + name})
		reasons[name] = builtReason("media", name)
	}
	var asked []string

	statuses, result, err := checkContainerImageUpdates(ctx, registry, recordingClassifier(&asked, reasons), targets, time.Now())
	if err != nil {
		t.Fatalf("checkContainerImageUpdates: %v", err)
	}
	if want := (DockerUpdateCheckResult{Built: 6, Checked: 6, Rebuilds: 1, Updates: 1}); result != want {
		t.Fatalf("result = %+v, want %+v: a moved base is a waiting rebuild, a failed base lookup no error", result, want)
	}
	// Each base is asked once, whatever number of images it is under.
	for _, ref := range []string{alpineBase.Ref, busyboxBase.Ref, "docker.io/library/gone:1"} {
		if n := countOf(registry.distributionCalls, ref); n != 1 {
			t.Fatalf("%s asked %d times, want once (calls %v)", ref, n, registry.distributionCalls)
		}
	}
	want := map[string]struct {
		available bool
		reason    string
	}{
		"norecord": {reason: builtReason("media", "norecord") + "; rebuild it once from LinuxIO to be told when its base images have newer versions"},
		"same":     {reason: builtReason("media", "same") + "; its base images have no newer version"},
		"moved":    {available: true, reason: "base image docker.io/library/alpine:3.20 has a newer version on its registry; " + builtReason("media", "moved")},
		"failed":   {reason: builtReason("media", "failed") + "; base image docker.io/library/gone:1 could not be checked: Error response from daemon: manifest unknown"},
		"scratch":  {reason: builtReason("media", "scratch") + "; it has no base image that can have a newer version (FROM scratch, or pinned by digest)"},
		"unread":   {reason: builtReason("media", "unread") + "; its base images could not be read from the build's output, so they are not watched"},
	}
	for _, status := range statuses {
		w := want[status.ContainerID]
		if status.CheckState != DockerUpdateCheckStateBuilt || status.Err != "" || status.UpdateAvailable != w.available || status.CheckReason != w.reason {
			t.Fatalf("%s = %+v, want built, available %v, reason %q", status.ContainerID, status, w.available, w.reason)
		}
	}
	// The occurrence of the alert is the digest the moved base has now.
	if moved := statuses[2]; moved.RemoteDigest != digest.FromString(alpineBase.Ref+"@"+alpineBase.Digest).String() {
		t.Fatalf("moved remote digest = %q", moved.RemoteDigest)
	}
}

func countOf(values []string, value string) int {
	n := 0
	for _, v := range values {
		if v == value {
			n++
		}
	}
	return n
}

// recordOf is the base images recorded for imageID, failing the test when
// there is no record.
func recordOf(t *testing.T, imageID string) []baseImage {
	t.Helper()
	record, ok := readBuiltImageRecords()[imageID]
	if !ok {
		t.Fatalf("no record for image %s", imageID)
	}
	return record.Bases
}

// fakeComposeBuildCLI puts a `docker` on PATH as fakeComposeCLI does, whose
// build writes output and succeeds.
func fakeComposeBuildCLI(t *testing.T, config, output string) string {
	t.Helper()
	dir := t.TempDir()
	args := filepath.Join(dir, "args")
	outputFile := filepath.Join(dir, "build-output")
	if err := os.WriteFile(outputFile, []byte(output), 0o600); err != nil {
		t.Fatal(err)
	}
	// PATH holds this folder alone, so the script uses builtins only.
	script := fmt.Sprintf(`#!/bin/sh
echo "$*" >> %[1]s
case "$*" in
*"config --format json"*) echo '%[2]s' ;;
*" build --pull "*) while IFS= read -r line; do printf '%%s\n' "$line"; done < %[3]s ;;
esac
`, args, config, outputFile)
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	return args
}

// movedWebStatus records the container of composeUpdateFixture as a check
// that found its base moved leaves it, with its alert.
func movedWebStatus(t *testing.T, before container.InspectResponse) {
	t.Helper()
	older := baseImage{Ref: alpineBase.Ref, Digest: digest.FromString("older").String()}
	if err := recordBuiltImage(t.Context(), builtImageRecord{ImageID: before.Image, Bases: []baseImage{older}, RecordedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	status := ImageUpdateStatus{ContainerID: before.ID, ContainerName: "web", CheckState: DockerUpdateCheckStateBuilt, ImageID: before.Image, ImageRef: before.Config.Image, UpdateAvailable: true, RemoteDigest: "sha256:r1"}
	if err := mergeUpdateStatuses(t.Context(), []ImageUpdateStatus{status}); err != nil {
		t.Fatal(err)
	}
	syncAvailableAlerts(t.Context(), []ImageUpdateStatus{status}, true)
}

// A rebuild records the bases its build resolved, also when the image did not
// change, and only then is the container no longer waiting for one. A build
// whose output was not understood is recorded as such, and says so.
func TestRebuildRecordsTheBaseImagesItResolved(t *testing.T) {
	for name, tc := range map[string]struct {
		output     string
		wantBases  []baseImage
		wantReason string
	}{
		"Compose's builder":     {measuredBuildOutputs["fully cached rebuild"], []baseImage{alpineBase}, builtBasesCurrent},
		"buildx and Bake":       {measuredBakeBuildOutputs["fully cached rebuild"], []baseImage{alpineBase}, builtBasesCurrent},
		"output not understood": {strings.ReplaceAll(measuredBakeBuildOutputs["fully cached rebuild"], "] FROM ", "] BASE "), []baseImage{}, builtBasesUnread},
	} {
		t.Run(name, func(t *testing.T) {
			withTempUpdateStatusPath(t)
			useTempAlertStore(t)
			fakeComposeBuildCLI(t, builtWebConfig, tc.output)
			before, fake, _ := composeUpdateFixture()
			fake.images = map[string]image.InspectResponse{"docker.io/library/nginx:latest": {ID: before.Image}}
			movedWebStatus(t, before)

			if _, err := rebuildComposeContainer(t.Context(), fake, composeRollback{}, before, composeTestTarget, "web", updateResultFor(before), nil); err != nil {
				t.Fatalf("rebuildComposeContainer: %v", err)
			}
			record := readBuiltImageRecords()[before.Image]
			if !slices.Equal(record.Bases, tc.wantBases) || record.BasesUnread != (tc.wantReason == builtBasesUnread) {
				t.Fatalf("record = %+v, want bases %+v", record, tc.wantBases)
			}
			status, _ := ReadStatusSnapshot().ForContainerName("web")
			if status.CheckState != DockerUpdateCheckStateBuilt || status.UpdateAvailable || status.CheckReason != builtReason("media", "web")+tc.wantReason {
				t.Fatalf("status = %+v, want built, reason ending %q", status, tc.wantReason)
			}
			if ids := alertIDs(t); len(ids) != 0 {
				t.Fatalf("alerts = %v, want the rebuild-available alert cleared", ids)
			}
		})
	}
}

// A rebuild whose new container was rolled back leaves the container on the
// old image, whose record still applies: it is still waiting for a rebuild.
func TestRebuildRolledBackKeepsTheOldRecord(t *testing.T) {
	withTempUpdateStatusPath(t)
	useTempAlertStore(t)
	fakeComposeBuildCLI(t, builtWebConfig, measuredBuildOutputs["first build"])
	before, fake, restored := composeUpdateFixture()
	commands := &composeRollbackFake{}
	rollback := commands.rollback(fake, map[string]composeServiceSettings{"web": {Image: "nginx:latest", Build: true}}, restored)
	movedWebStatus(t, before)
	oldRecord := recordOf(t, before.Image)

	if _, err := rebuildComposeContainer(t.Context(), fake, rollback, before, composeTestTarget, "web", updateResultFor(before), nil); !errors.As(err, new(rolledBackError)) {
		t.Fatalf("error = %v, want a rolled back rebuild", err)
	}
	if bases := recordOf(t, before.Image); !slices.Equal(bases, oldRecord) {
		t.Fatalf("old record = %+v, want it kept as %+v", bases, oldRecord)
	}
	if bases := recordOf(t, "sha256:new"); !slices.Equal(bases, []baseImage{alpineBase}) {
		t.Fatalf("new record = %+v", bases)
	}
	if status, _ := ReadStatusSnapshot().ForContainerName("web"); !status.UpdateAvailable {
		t.Fatalf("status = %+v, want it still waiting for a rebuild", status)
	}
	if ids := alertIDs(t); len(ids) != 1 || ids[0] != "docker-update/available-web" {
		t.Fatalf("alerts = %v, want the rebuild-available alert kept", ids)
	}
}

// A plain update of a built container whose base moved finds what the check
// found: nothing to update, the status still waiting for a rebuild, and its
// alert kept.
func TestUpdateCandidateOfABuiltContainerWithANewerBase(t *testing.T) {
	withTempUpdateStatusPath(t)
	useTempAlertStore(t)
	inspect := composeWebInspect()
	movedWebStatus(t, inspect)

	candidate, err := inspectContainerUpdateCandidate(t.Context(), builtRegistry(inspect.Image), builtClassifierFor(inspect, true), inspect)
	if err != nil || candidate.needsUpdate || !candidate.built || !candidate.baseMoved {
		t.Fatalf("candidate = %+v, error = %v; want a built container waiting for a rebuild", candidate, err)
	}
	status, _ := ReadStatusSnapshot().ForContainerName("web")
	if status.CheckState != DockerUpdateCheckStateBuilt || !status.UpdateAvailable || !strings.HasPrefix(status.CheckReason, "base image docker.io/library/alpine:3.20 has a newer version") {
		t.Fatalf("status = %+v, want it still waiting for a rebuild", status)
	}
	if ids := alertIDs(t); len(ids) != 1 || ids[0] != "docker-update/available-web" {
		t.Fatalf("alerts = %v, want the rebuild-available alert kept", ids)
	}
}

// The scheduled pass reports a built container whose base moved, and never
// updates or builds it: the check raises its alert, with its own wording, and
// counts it as waiting; the update step leaves it and its alert alone. Once a
// rebuild recorded the new base, the next check clears the alert.
func TestScheduledPassNeverUpdatesOrBuildsABuiltContainerWithANewerBase(t *testing.T) {
	withTempUpdateStatusPath(t)
	useTempAlertStore(t)
	args := fakeComposeCLI(t, builtWebConfig, 0, 0)
	ctx := t.Context()
	inspect := composeWebInspect()
	summary := container.Summary{ID: inspect.ID, Names: []string{inspect.Name}, Image: inspect.Config.Image, ImageID: inspect.Image, Labels: inspect.Config.Labels, State: container.StateRunning}
	older := baseImage{Ref: alpineBase.Ref, Digest: digest.FromString("older").String()}
	if err := recordBuiltImage(ctx, builtImageRecord{ImageID: inspect.Image, Bases: []baseImage{older}, RecordedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	registry := newRegistryLookupCache(builtRegistry(inspect.Image))
	classify := builtClassifierFor(inspect, true)

	checked, err := runScheduledUpdateCheck(ctx, registry, classify, []container.Summary{summary}, true)
	if err != nil || checked.Updates != 1 || checked.Built != 1 {
		t.Fatalf("check = %+v, %v; want one built container waiting", checked, err)
	}
	doc, err := alerts.Load()
	if err != nil || len(doc.Alerts) != 1 || doc.Alerts[0].ID != "docker-update/available-web" || doc.Alerts[0].Title != "Docker rebuild available: web" ||
		!strings.Contains(doc.Alerts[0].Message, "A base image of nginx:latest has a newer version") {
		t.Fatalf("alerts = %+v, %v; want the rebuild-available alert", doc.Alerts, err)
	}
	if waiting, raise := passNotice(reportingDocker(alerts.ReportEveryRun, false), nil, checked, 0); !raise || waiting != 1 {
		t.Fatalf("pass notice = %d, %v; want one waiting", waiting, raise)
	}

	state := newScheduledUpdateState(ctx, inspectingDockerClient(t, inspect))
	state.registry, state.classify = registry, classify
	updated := false
	state.composeUpdate = func(context.Context, ComposeTarget, []string, ComposeLineEmitter) error {
		updated = true
		return nil
	}
	if err := state.prepare(summary); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if err := state.applyComposeGroups(); err != nil {
		t.Fatalf("applyComposeGroups: %v", err)
	}
	if updated || len(state.composeGroups) != 0 || len(state.outcomes) != 0 || len(state.errs) != 0 {
		t.Fatalf("pass updated %v, groups %v, outcomes %+v, errors %v; want the container left alone", updated, state.composeGroups, state.outcomes, state.errs)
	}
	if commands := composeCommandsRun(t, args); commands != nil {
		t.Fatalf("docker ran %q, want nothing built", commands)
	}
	if status, _ := ReadStatusSnapshot().ForContainerName("web"); !status.UpdateAvailable || status.CheckState != DockerUpdateCheckStateBuilt {
		t.Fatalf("status = %+v, want it still waiting for a rebuild", status)
	}
	if ids := alertIDs(t); len(ids) != 1 {
		t.Fatalf("alerts = %v, want the rebuild-available alert kept", ids)
	}

	if err := recordBuiltImage(ctx, builtImageRecord{ImageID: inspect.Image, Bases: []baseImage{alpineBase}, RecordedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if checked, err := runScheduledUpdateCheck(ctx, registry, classify, []container.Summary{summary}, true); err != nil || checked.Updates != 0 {
		t.Fatalf("check after the rebuild = %+v, %v", checked, err)
	}
	if ids := alertIDs(t); len(ids) != 0 {
		t.Fatalf("alerts = %v, want the alert cleared", ids)
	}
}

// inspectingDockerClient is a Docker client whose daemon answers the
// inspection of each of containers, and fails the test on any other request.
func inspectingDockerClient(t *testing.T, containers ...container.InspectResponse) *client.Client {
	t.Helper()
	const version = "1.51"
	cli, err := client.New(client.WithAPIVersion(version), client.WithHTTPClient(&http.Client{
		Transport: dockerAPITransport(func(r *http.Request) (*http.Response, error) {
			for _, inspect := range containers {
				if r.Method != http.MethodGet || r.URL.Path != "/v"+version+"/containers/"+inspect.ID+"/json" {
					continue
				}
				body, err := json.Marshal(map[string]any{
					"Id":     inspect.ID,
					"Name":   inspect.Name,
					"Image":  inspect.Image,
					"State":  map[string]any{"Status": string(inspect.State.Status), "Running": inspect.State.Running},
					"Config": map[string]any{"Image": inspect.Config.Image, "Labels": inspect.Config.Labels},
				})
				if err != nil {
					return nil, err
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(body))}, nil
			}
			t.Errorf("unexpected Docker request %s %s", r.Method, r.URL.Path)
			return &http.Response{StatusCode: http.StatusInternalServerError, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"message":"unexpected request"}`))}, nil
		}),
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	return cli
}

type dockerAPITransport func(*http.Request) (*http.Response, error)

func (f dockerAPITransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
