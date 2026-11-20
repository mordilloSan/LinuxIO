package dockerupdate

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
)

func TestComposeServiceSettingsReadsTheEffectiveConfiguration(t *testing.T) {
	dir := t.TempDir()
	args := filepath.Join(dir, "args")
	// Compose writes warnings to stderr; only stdout is the configuration.
	script := "#!/bin/sh\necho \"$@\" > " + args + "\n" +
		"echo 'level=warning msg=\"the attribute version is obsolete\"' >&2\n" +
		`echo '{
  "name": "media",
  "services": {
    "web": {"image": "nginx:latest", "pull_policy": "always"},
    "app": {"build": {"context": "/srv/app"}, "image": "media-app"},
    "db": {"image": "postgres:16"}
  }
}'
`
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	// The services are named: Compose leaves out a service of a profile that
	// is not active unless it is named.
	got, err := composeServiceSettingsOf(t.Context(), ComposeTarget{Name: "media", ConfigFiles: []string{"compose.yml"}}, []string{"web", "app", "db"})
	if err != nil {
		t.Fatalf("composeServiceSettingsOf: %v", err)
	}
	want := map[string]composeServiceSettings{
		"web": {Image: "nginx:latest", PullPolicy: "always"},
		"app": {Image: "media-app", Build: true},
		"db":  {Image: "postgres:16"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("settings = %+v, want %+v", got, want)
	}
	called, _ := os.ReadFile(args)
	if !strings.HasSuffix(strings.TrimSpace(string(called)), "--file compose.yml config --format json web app db") {
		t.Fatalf("docker called with %q", called)
	}

	failing := "#!/bin/sh\necho 'open compose.yml: no such file or directory' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(failing), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := composeServiceSettingsOf(t.Context(), ComposeTarget{Name: "media", ConfigFiles: []string{"compose.yml"}}, []string{"web"}); err == nil || !strings.Contains(err.Error(), "no such file or directory") {
		t.Fatalf("error = %v, want Compose's stderr in it", err)
	}
}

// composeRollbackFake answers a rollback's Compose commands: settings is each
// service's effective configuration, and restore records the services it
// brings up and puts the restored containers in place.
type composeRollbackFake struct {
	settingsFor [][]string // the services each settings read named
	restores    [][]string
	restoreErr  error
	// restoreBlocks keeps the restore going until its context ends;
	// restoreDelay makes it take that long.
	restoreBlocks bool
	restoreDelay  time.Duration
	// restoreCtx is the state of the context the services were brought up
	// with, read after onRestore ran.
	restoreCtxErr     error
	restoreCtxBounded bool
	// onRestore runs as the services are brought up.
	onRestore func()
}

func (f *composeRollbackFake) rollback(cli *fakeNativeUpdateClient, settings map[string]composeServiceSettings, restored map[string]container.InspectResponse) composeRollback {
	return composeRollback{
		cli: cli,
		settings: func(_ context.Context, _ ComposeTarget, services []string) (map[string]composeServiceSettings, error) {
			f.settingsFor = append(f.settingsFor, services)
			return settings, nil
		},
		restore: func(ctx context.Context, _ ComposeTarget, services []string, _ ComposeLineEmitter) error {
			f.restores = append(f.restores, services)
			if f.onRestore != nil {
				f.onRestore()
			}
			if f.restoreBlocks {
				<-ctx.Done()
				return ctx.Err()
			}
			time.Sleep(f.restoreDelay)
			f.restoreCtxErr = ctx.Err()
			_, f.restoreCtxBounded = ctx.Deadline()
			if f.restoreErr != nil {
				return f.restoreErr
			}
			maps.Copy(cli.inspectResults, restored)
			return nil
		},
	}
}

func tagCalls(calls []string) []string {
	var tags []string
	for _, call := range calls {
		if strings.HasPrefix(call, "tag:") {
			tags = append(tags, call)
		}
	}
	return tags
}

// composeUpdateFixture is a Compose service "web" on nginx:latest whose
// update pulled sha256:new, and whose new container exits.
func composeUpdateFixture() (before container.InspectResponse, fake *fakeNativeUpdateClient, restored map[string]container.InspectResponse) {
	before = standaloneTestInspect()
	fake = newStandaloneUpdateFake()
	failing := container.InspectResponse{ID: "new-web", Name: "/web", Image: "sha256:new", State: &container.State{Status: container.StateExited, ExitCode: 1}}
	fake.inspectResults["web"] = failing
	fake.inspectResults["new-web"] = failing
	back := readyContainer("restored-web", before.Image)
	return before, fake, map[string]container.InspectResponse{"web": back, "restored-web": back}
}

var composeTestTarget = ComposeTarget{Name: "media", ConfigFiles: []string{"compose.yml"}}

func pulledAndUp(context.Context, ComposeTarget, string, ComposeLineEmitter) error { return nil }

func TestUpdateComposeContainerRollsBackAServiceThatIsNotReady(t *testing.T) {
	withTempUpdateStatusPath(t)
	before, fake, restored := composeUpdateFixture()
	commands := &composeRollbackFake{}
	rollback := commands.rollback(fake, map[string]composeServiceSettings{"web": {Image: "nginx:latest"}}, restored)

	result, err := updateComposeContainerWithRunner(t.Context(), fake, rollback, before, composeTestTarget, "web", updateResultFor(before), pulledAndUp, nil)
	var rolledBack rolledBackError
	if !errors.As(err, &rolledBack) || result.Updated {
		t.Fatalf("result = %+v, error = %v; want a rolled back update", result, err)
	}
	for _, want := range []string{`verify updated Compose container "web"`, `container state is "exited"`, "rolled back to image old"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want it to contain %q", err, want)
		}
	}
	// The old image is named by the ID read before the pull.
	if tags := tagCalls(fake.calls); !reflect.DeepEqual(tags, []string{"tag:sha256:old:docker.io/library/nginx:latest"}) {
		t.Fatalf("tags = %v, want the tag put back on the old image", tags)
	}
	if !reflect.DeepEqual(commands.restores, [][]string{{"web"}}) || !reflect.DeepEqual(commands.settingsFor, [][]string{{"web"}}) {
		t.Fatalf("restored services = %v, settings read for %v", commands.restores, commands.settingsFor)
	}
	// The restored container is waited for.
	if fake.calls[len(fake.calls)-1] != "inspect-container:restored-web" {
		t.Fatalf("calls = %v, want the restored container's readiness read last", fake.calls)
	}
}

func TestUpdateComposeContainerIsNotRolledBackWhereTheTagCannotBePutBack(t *testing.T) {
	const pinned = "nginx@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	for name, tc := range map[string]struct {
		image    string
		settings composeServiceSettings
		pulled   string
		reason   string
	}{
		"pull_policy always":             {settings: composeServiceSettings{Image: "nginx:latest", PullPolicy: "always"}, reason: "pull_policy: always"},
		"built, with pull_policy always": {settings: composeServiceSettings{Build: true, PullPolicy: "always"}, reason: "pull_policy: always"},
		"an image pinned":                {image: pinned, settings: composeServiceSettings{Image: pinned}, reason: "pinned by digest"},
		"another image now":              {settings: composeServiceSettings{Image: "nginx:1.29"}, reason: `names image "nginx:1.29"`},
		"the tag did not move":           {settings: composeServiceSettings{Image: "nginx:latest"}, pulled: "sha256:old", reason: "still names the image"},
	} {
		t.Run(name, func(t *testing.T) {
			withTempUpdateStatusPath(t)
			before, fake, restored := composeUpdateFixture()
			if tc.image != "" {
				before.Config.Image = tc.image
			}
			if tc.pulled != "" {
				fake.pulledImage.ID = tc.pulled
			}
			commands := &composeRollbackFake{}
			rollback := commands.rollback(fake, map[string]composeServiceSettings{"web": tc.settings}, restored)

			_, err := updateComposeContainerWithRunner(t.Context(), fake, rollback, before, composeTestTarget, "web", updateResultFor(before), pulledAndUp, nil)
			if err == nil || errors.As(err, new(rolledBackError)) || !strings.Contains(err.Error(), "not rolled back") || !strings.Contains(err.Error(), tc.reason) {
				t.Fatalf("error = %v, want a failed update not rolled back because of %q", err, tc.reason)
			}
			if tags := tagCalls(fake.calls); len(tags) != 0 || len(commands.restores) != 0 {
				t.Fatalf("tags %v, restores %v; want the host left alone", tags, commands.restores)
			}
		})
	}
}

// A service with a build section takes the tag back like one that is pulled,
// whether its files name the image or leave Compose its default name.
func TestUpdateComposeContainerRollsBackAServiceWithABuildSection(t *testing.T) {
	for name, tc := range map[string]struct {
		image    string // what the container was created from
		settings composeServiceSettings
		tag      string
	}{
		"an image and a build section": {image: "nginx:latest", settings: composeServiceSettings{Image: "nginx:latest", Build: true}, tag: "docker.io/library/nginx:latest"},
		"a build section alone":        {image: "media-web", settings: composeServiceSettings{Build: true}, tag: "docker.io/library/media-web:latest"},
	} {
		t.Run(name, func(t *testing.T) {
			withTempUpdateStatusPath(t)
			before, fake, restored := composeUpdateFixture()
			before.Config.Image = tc.image
			commands := &composeRollbackFake{}
			rollback := commands.rollback(fake, map[string]composeServiceSettings{"web": tc.settings}, restored)

			_, err := updateComposeContainerWithRunner(t.Context(), fake, rollback, before, composeTestTarget, "web", updateResultFor(before), pulledAndUp, nil)
			if !errors.As(err, new(rolledBackError)) {
				t.Fatalf("error = %v, want a rolled back update", err)
			}
			if tags := tagCalls(fake.calls); !reflect.DeepEqual(tags, []string{"tag:sha256:old:" + tc.tag}) {
				t.Fatalf("tags = %v, want the tag %s put back on the old image", tags, tc.tag)
			}
			if !reflect.DeepEqual(commands.restores, [][]string{{"web"}}) {
				t.Fatalf("restored services = %v, want web", commands.restores)
			}
		})
	}
}

// A rollback's `up` neither pulls nor builds: a service with a build section,
// or with pull_policy: build, would otherwise build the image it undoes.
func TestComposeUpFromLocalImagesPullsAndBuildsNothing(t *testing.T) {
	dir := t.TempDir()
	args := filepath.Join(dir, "args")
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte("#!/bin/sh\necho \"$*\" > "+args+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	if err := composeUpFromLocalImages(t.Context(), composeTestTarget, []string{"web", "worker"}, nil); err != nil {
		t.Fatalf("composeUpFromLocalImages: %v", err)
	}
	called, _ := os.ReadFile(args)
	if got := strings.TrimSpace(string(called)); !strings.HasSuffix(got, "--file compose.yml up -d --no-deps --pull never --no-build web worker") {
		t.Fatalf("docker called with %q", got)
	}
}

func TestUpdateComposeContainerReportsAFailedRollback(t *testing.T) {
	for name, tc := range map[string]struct {
		restoreErr  error
		tagErr      error
		notRestored bool
		wantTags    []string
		want        string
	}{
		// What the rollback put on the host is undone where it can be: the
		// tag goes back on the new image.
		"up fails": {
			restoreErr: errors.New("port is already allocated"),
			wantTags:   []string{"tag:sha256:old:docker.io/library/nginx:latest", "tag:sha256:new:docker.io/library/nginx:latest"},
			want:       "port is already allocated",
		},
		// A tag that was not moved leaves nothing to undo.
		"the tag fails": {
			tagErr:   errors.New("no such image"),
			wantTags: []string{"tag:sha256:old:docker.io/library/nginx:latest"},
			want:     "no such image",
		},
		// The tag and the containers agree on the old image: nothing to undo.
		"the restored container is not ready": {
			notRestored: true,
			wantTags:    []string{"tag:sha256:old:docker.io/library/nginx:latest"},
			want:        `restored container "web"`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			withTempUpdateStatusPath(t)
			before, fake, restored := composeUpdateFixture()
			fake.tagErr = tc.tagErr
			if tc.notRestored {
				restored = nil
			}
			commands := &composeRollbackFake{restoreErr: tc.restoreErr}
			rollback := commands.rollback(fake, map[string]composeServiceSettings{"web": {Image: "nginx:latest"}}, restored)

			_, err := updateComposeContainerWithRunner(t.Context(), fake, rollback, before, composeTestTarget, "web", updateResultFor(before), pulledAndUp, nil)
			if err == nil || errors.As(err, new(rolledBackError)) {
				t.Fatalf("error = %v, want a failed rollback", err)
			}
			// Both failures are reported.
			for _, want := range []string{`container state is "exited"`, "rollback failed", tc.want} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error = %q, want it to contain %q", err, want)
				}
			}
			if tags := tagCalls(fake.calls); !reflect.DeepEqual(tags, tc.wantTags) {
				t.Fatalf("tags = %v, want %v", tags, tc.wantTags)
			}
		})
	}
}

// A request canceled while it waits is not rolled back: what the new
// container would have done is unknown. A rollback that has begun is not
// canceled half-way with the request.
func TestUpdateComposeContainerRollbackAndCancellation(t *testing.T) {
	t.Run("canceled before the verdict", func(t *testing.T) {
		withTempUpdateStatusPath(t)
		before, fake, restored := composeUpdateFixture()
		commands := &composeRollbackFake{}
		rollback := commands.rollback(fake, map[string]composeServiceSettings{"web": {Image: "nginx:latest"}}, restored)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		if _, err := updateComposeContainerWithRunner(ctx, fake, rollback, before, composeTestTarget, "web", updateResultFor(before), pulledAndUp, nil); err == nil {
			t.Fatal("a canceled update reported success")
		}
		if tags := tagCalls(fake.calls); len(tags) != 0 || len(commands.restores) != 0 {
			t.Fatalf("tags %v, restores %v; want no rollback", tags, commands.restores)
		}
	})
	t.Run("canceled during the rollback", func(t *testing.T) {
		withTempUpdateStatusPath(t)
		before, fake, restored := composeUpdateFixture()
		ctx, cancel := context.WithCancel(t.Context())
		commands := &composeRollbackFake{onRestore: cancel}
		rollback := commands.rollback(fake, map[string]composeServiceSettings{"web": {Image: "nginx:latest"}}, restored)

		_, err := updateComposeContainerWithRunner(ctx, fake, rollback, before, composeTestTarget, "web", updateResultFor(before), pulledAndUp, nil)
		if !errors.As(err, new(rolledBackError)) {
			t.Fatalf("error = %v, want the rollback finished", err)
		}
		if commands.restoreCtxErr != nil || !commands.restoreCtxBounded {
			t.Fatalf("restore context: err %v, bounded %v; want detached from the request and bounded", commands.restoreCtxErr, commands.restoreCtxBounded)
		}
	})
}

// Services of one project updated to the same image go back together: one
// left on the new image while the tag names the old one would be moved back
// silently by its next `up`.
func TestScheduledComposeRollsBackTheServicesSharingTheImage(t *testing.T) {
	withTempUpdateStatusPath(t)
	useTempAlertStore(t)
	state := newScheduledUpdateState(t.Context(), nil)
	state.alertReport = alerts.ReportChanges
	named := func(id, name, imageID, ref string, state *container.State) container.InspectResponse {
		return container.InspectResponse{ID: id, Name: "/" + name, Image: imageID, State: state, Config: &container.Config{Image: ref}}
	}
	healthy := func() *container.State {
		return &container.State{Status: container.StateRunning, Running: true, Health: &container.Health{Status: container.Healthy}}
	}
	app := named("app-old", "app", "sha256:app-old", "example/app:1", healthy())
	worker := named("worker-old", "worker", "sha256:app-old", "example/app:1", healthy())
	db := named("db-old", "db", "sha256:db-old", "postgres:16", healthy())

	fake := newStandaloneUpdateFake()
	fake.images = map[string]image.InspectResponse{"docker.io/example/app:1": {ID: "sha256:app-new"}}
	for _, c := range []container.InspectResponse{
		named("app-new", "app", "sha256:app-new", "example/app:1", &container.State{Status: container.StateExited, ExitCode: 1}),
		named("worker-new", "worker", "sha256:app-new", "example/app:1", healthy()),
		named("db-new", "db", "sha256:db-new", "postgres:16", healthy()),
	} {
		fake.inspectResults[strings.TrimPrefix(c.Name, "/")] = c
		fake.inspectResults[c.ID] = c
	}
	restored := map[string]container.InspectResponse{}
	for _, c := range []container.InspectResponse{
		named("app-back", "app", "sha256:app-old", "example/app:1", healthy()),
		named("worker-back", "worker", "sha256:app-old", "example/app:1", healthy()),
	} {
		restored[strings.TrimPrefix(c.Name, "/")] = c
		restored[c.ID] = c
	}
	commands := &composeRollbackFake{}
	state.readiness = fake
	state.rollback = commands.rollback(fake, map[string]composeServiceSettings{
		"app":    {Image: "example/app:1"},
		"worker": {Image: "example/app:1"},
		"db":     {Image: "postgres:16"},
	}, restored)
	group := &scheduledComposeTarget{
		target:   composeTestTarget,
		services: []string{"app", "worker", "db"},
		before:   []container.InspectResponse{app, worker, db},
	}

	if err := state.verifyComposeGroup(group); err != nil {
		t.Fatalf("verifyComposeGroup: %v", err)
	}
	if !reflect.DeepEqual(commands.restores, [][]string{{"app", "worker"}}) {
		t.Fatalf("restored services = %v, want app and worker together", commands.restores)
	}
	if tags := tagCalls(fake.calls); !reflect.DeepEqual(tags, []string{"tag:sha256:app-old:docker.io/example/app:1"}) {
		t.Fatalf("tags = %v", tags)
	}
	if len(state.outcomes) != 3 {
		t.Fatalf("outcomes = %+v, want app, worker and db", state.outcomes)
	}
	for i, name := range []string{"app", "worker"} {
		outcome := state.outcomes[i]
		if outcome.Name != name || !errors.As(outcome.Err, new(rolledBackError)) {
			t.Fatalf("outcome %d = %+v, want %s rolled back", i, outcome, name)
		}
	}
	if !strings.Contains(state.outcomes[1].Err.Error(), "app runs the same image") {
		t.Fatalf("worker's error = %q, want it to name the service that failed", state.outcomes[1].Err)
	}
	if db := state.outcomes[2]; db.Name != "db" || db.Err != nil || db.NewImageID != "sha256:db-new" {
		t.Fatalf("db outcome = %+v, want it updated", db)
	}
	// A rolled back service's old image is in use again: not for the cleanup.
	if !reflect.DeepEqual(state.oldImageIDs, []string{"sha256:db-old"}) {
		t.Fatalf("old images = %v", state.oldImageIDs)
	}
	if ids := alertIDs(t); !reflect.DeepEqual(slices.Sorted(slices.Values(ids)), []string{"docker-update/failed-app", "docker-update/failed-worker", "docker-update/updated-db"}) {
		t.Fatalf("alerts = %v", ids)
	}
}

// A service that cannot be rolled back leaves the others that share its
// image on the new one, where they became ready.
func TestScheduledComposeKeepsReadyServicesWhenNoRollbackIsPossible(t *testing.T) {
	withTempUpdateStatusPath(t)
	useTempAlertStore(t)
	state := newScheduledUpdateState(t.Context(), nil)
	app := standaloneTestInspect()
	app.ID, app.Name = "app-old", "/app"
	worker := standaloneTestInspect()
	worker.ID, worker.Name = "worker-old", "/worker"
	fake := newStandaloneUpdateFake()
	newApp := container.InspectResponse{ID: "app-new", Name: "/app", Image: "sha256:new", State: &container.State{Status: container.StateExited}}
	newWorker := readyContainer("worker-new", "sha256:new")
	newWorker.Name = "/worker"
	maps.Copy(fake.inspectResults, map[string]container.InspectResponse{"app": newApp, "app-new": newApp, "worker": newWorker, "worker-new": newWorker})
	commands := &composeRollbackFake{}
	state.readiness = fake
	state.rollback = commands.rollback(fake, map[string]composeServiceSettings{
		"app":    {Image: "nginx:latest", PullPolicy: "always"},
		"worker": {Image: "nginx:latest", PullPolicy: "always"},
	}, nil)
	group := &scheduledComposeTarget{target: composeTestTarget, services: []string{"app", "worker"}, before: []container.InspectResponse{app, worker}}

	if err := state.verifyComposeGroup(group); err != nil {
		t.Fatalf("verifyComposeGroup: %v", err)
	}
	if len(commands.restores) != 0 || len(state.outcomes) != 2 {
		t.Fatalf("restores %v, outcomes %+v", commands.restores, state.outcomes)
	}
	if err := state.outcomes[0].Err; err == nil || !strings.Contains(err.Error(), "not rolled back") {
		t.Fatalf("app's error = %v, want it not rolled back", err)
	}
	if state.outcomes[1].Err != nil || state.outcomes[1].NewImageID != "sha256:new" {
		t.Fatalf("worker outcome = %+v, want it updated", state.outcomes[1])
	}
}

func TestRolledBackUpdateAlertSaysSo(t *testing.T) {
	useTempAlertStore(t)
	err := rolledBackError{err: errors.New("container health is \"unhealthy\""), to: "image 1a2b3c4d5e6f"}
	raiseDockerUpdateAlert(t.Context(), "/web", "nginx:latest", err)
	doc, loadErr := alerts.Load()
	if loadErr != nil || len(doc.Alerts) != 1 {
		t.Fatalf("alerts = %+v, %v", doc.Alerts, loadErr)
	}
	stored := doc.Alerts[0]
	if stored.Title != "Docker update rolled back: web" || !strings.Contains(stored.Message, "rolled back to image 1a2b3c4d5e6f") {
		t.Fatalf("alert = %q / %q, want it to say the update was rolled back", stored.Title, stored.Message)
	}
}

// A new container that was still coming up when the wait ended may already
// have begun to change its data, such as a migration at start: it is not
// rolled back, and the reason says why.
func TestUpdateComposeContainerIsNotRolledBackWhileTheContainerIsComingUp(t *testing.T) {
	for name, state := range map[string]*container.State{
		"health starting": {Status: container.StateRunning, Running: true, Health: &container.Health{Status: "starting"}},
		// A start time that cannot be read never settles.
		"running, not settled": {Status: container.StateRunning, Running: true},
	} {
		t.Run(name, func(t *testing.T) {
			withTempUpdateStatusPath(t)
			synctest.Test(t, func(t *testing.T) {
				before, fake, restored := composeUpdateFixture()
				starting := container.InspectResponse{ID: "new-web", Name: "/web", Image: "sha256:new", State: state}
				fake.inspectResults["web"], fake.inspectResults["new-web"] = starting, starting
				commands := &composeRollbackFake{}
				rollback := commands.rollback(fake, map[string]composeServiceSettings{"web": {Image: "nginx:latest"}}, restored)

				_, err := updateComposeContainerWithRunner(t.Context(), fake, rollback, before, composeTestTarget, "web", updateResultFor(before), pulledAndUp, nil)
				for _, want := range []string{"not ready after 1m30s", "not rolled back", "had not exited"} {
					if err == nil || !strings.Contains(err.Error(), want) {
						t.Fatalf("error = %v, want it to contain %q", err, want)
					}
				}
				if tags := tagCalls(fake.calls); len(tags) != 0 || len(commands.restores) != 0 {
					t.Fatalf("tags %v, restores %v; want no rollback", tags, commands.restores)
				}
			})
		})
	}
}

// A rollback is checked: the restored container must run the old image.
func TestUpdateComposeContainerRollbackThatDidNotLandFails(t *testing.T) {
	withTempUpdateStatusPath(t)
	before, fake, _ := composeUpdateFixture()
	stillNew := readyContainer("restored-web", "sha256:new")
	commands := &composeRollbackFake{}
	rollback := commands.rollback(fake, map[string]composeServiceSettings{"web": {Image: "nginx:latest"}}, map[string]container.InspectResponse{"web": stillNew, "restored-web": stillNew})

	_, err := updateComposeContainerWithRunner(t.Context(), fake, rollback, before, composeTestTarget, "web", updateResultFor(before), pulledAndUp, nil)
	if err == nil || errors.As(err, new(rolledBackError)) || !strings.Contains(err.Error(), "rollback failed") || !strings.Contains(err.Error(), "runs image new") {
		t.Fatalf("error = %v, want a failed rollback naming the image the container runs", err)
	}
}

// The tag goes back on the new image after a failed up even when the
// rollback's own time ran out during it.
func TestUpdateComposeContainerPutsTheTagBackAfterTheRollbacksTimeRanOut(t *testing.T) {
	withTempUpdateStatusPath(t)
	synctest.Test(t, func(t *testing.T) {
		before, fake, restored := composeUpdateFixture()
		commands := &composeRollbackFake{restoreBlocks: true}
		rollback := commands.rollback(fake, map[string]composeServiceSettings{"web": {Image: "nginx:latest"}}, restored)

		_, err := updateComposeContainerWithRunner(t.Context(), fake, rollback, before, composeTestTarget, "web", updateResultFor(before), pulledAndUp, nil)
		if err == nil || !strings.Contains(err.Error(), "rollback failed") {
			t.Fatalf("error = %v, want a failed rollback", err)
		}
		want := []string{"tag:sha256:old:docker.io/library/nginx:latest", "tag:sha256:new:docker.io/library/nginx:latest"}
		if tags := tagCalls(fake.calls); !reflect.DeepEqual(tags, want) || fake.tagCtxErrs[1] != nil {
			t.Fatalf("tags = %v with contexts %v, want %v, the last on a live context", tags, fake.tagCtxErrs, want)
		}
	})
}

// When the rollback's own limit ends the wait for the restored container, the
// error says so: a bare deadline error would read as "operation timed out".
func TestUpdateComposeContainerSaysTheRollbacksLimitEndedTheWait(t *testing.T) {
	withTempUpdateStatusPath(t)
	synctest.Test(t, func(t *testing.T) {
		before, fake, _ := composeUpdateFixture()
		starting := readyContainer("restored-web", before.Image)
		starting.State.Health.Status = "starting"
		commands := &composeRollbackFake{restoreDelay: composeRollbackTimeout - 5*time.Second}
		rollback := commands.rollback(fake, map[string]composeServiceSettings{"web": {Image: "nginx:latest"}}, map[string]container.InspectResponse{"web": starting, "restored-web": starting})

		_, err := updateComposeContainerWithRunner(t.Context(), fake, rollback, before, composeTestTarget, "web", updateResultFor(before), pulledAndUp, nil)
		if err == nil || errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "rollback's limit") {
			t.Fatalf("error = %v, want it to name the rollback's limit and not wrap a deadline error", err)
		}
	})
}

// scheduledComposeFixture is a pass's Compose group whose services app and
// worker are named by refs and ran old: app's new container exits, worker's
// is ready. Restored containers run old.
func scheduledComposeFixture(t *testing.T, refs, old [2]string) (*scheduledUpdateState, *scheduledComposeTarget, *fakeNativeUpdateClient, *composeRollbackFake) {
	t.Helper()
	withTempUpdateStatusPath(t)
	useTempAlertStore(t)
	state := newScheduledUpdateState(t.Context(), nil)
	named := func(id, name, imageID, ref string, state *container.State) container.InspectResponse {
		return container.InspectResponse{ID: id, Name: "/" + name, Image: imageID, State: state, Config: &container.Config{Image: ref}}
	}
	healthy := func() *container.State {
		return &container.State{Status: container.StateRunning, Running: true, Health: &container.Health{Status: container.Healthy}}
	}
	fake := newStandaloneUpdateFake()
	fake.images = map[string]image.InspectResponse{"docker.io/example/app:latest": {ID: "sha256:app-new"}}
	for _, c := range []container.InspectResponse{
		named("app-new", "app", "sha256:app-new", refs[0], &container.State{Status: container.StateExited, ExitCode: 1}),
		named("worker-new", "worker", "sha256:app-new", refs[1], healthy()),
	} {
		fake.inspectResults[strings.TrimPrefix(c.Name, "/")] = c
		fake.inspectResults[c.ID] = c
	}
	restored := map[string]container.InspectResponse{}
	for _, c := range []container.InspectResponse{
		named("app-back", "app", old[0], refs[0], healthy()),
		named("worker-back", "worker", old[1], refs[1], healthy()),
	} {
		restored[strings.TrimPrefix(c.Name, "/")] = c
		restored[c.ID] = c
	}
	commands := &composeRollbackFake{}
	state.readiness = fake
	state.rollback = commands.rollback(fake, map[string]composeServiceSettings{"app": {Image: refs[0]}, "worker": {Image: refs[1]}}, restored)
	group := &scheduledComposeTarget{
		target:   composeTestTarget,
		services: []string{"app", "worker"},
		before:   []container.InspectResponse{named("app-old", "app", old[0], refs[0], healthy()), named("worker-old", "worker", old[1], refs[1], healthy())},
	}
	return state, group, fake, commands
}

// Two spellings of one tag are one tag: one rollback moves it, and what it
// named is read once, before any rollback.
func TestScheduledComposeRollsBackOneTagSpelledTwoWays(t *testing.T) {
	state, group, fake, commands := scheduledComposeFixture(t, [2]string{"example/app", "example/app:latest"}, [2]string{"sha256:app-old", "sha256:app-old"})

	if err := state.verifyComposeGroup(group); err != nil {
		t.Fatalf("verifyComposeGroup: %v", err)
	}
	if !reflect.DeepEqual(commands.restores, [][]string{{"app", "worker"}}) {
		t.Fatalf("restored services = %v, want app and worker in one rollback", commands.restores)
	}
	if tags := tagCalls(fake.calls); !reflect.DeepEqual(tags, []string{"tag:sha256:app-old:docker.io/example/app:latest"}) {
		t.Fatalf("tags = %v", tags)
	}
	if reads := slices.Collect(func(yield func(string) bool) {
		for _, call := range fake.calls {
			if call == "inspect-image:docker.io/example/app:latest" && !yield(call) {
				return
			}
		}
	}); len(reads) != 1 {
		t.Fatalf("the tag was read %d times, want once", len(reads))
	}
	for _, outcome := range state.outcomes {
		if !errors.As(outcome.Err, new(rolledBackError)) {
			t.Fatalf("outcome %+v, want rolled back", outcome)
		}
	}
}

// Services of a project under one tag that ran different images cannot all go
// back with one tag: none is rolled back.
func TestScheduledComposeDoesNotRollBackATagWhoseServicesRanDifferentImages(t *testing.T) {
	state, group, fake, commands := scheduledComposeFixture(t, [2]string{"example/app:latest", "example/app:latest"}, [2]string{"sha256:app-old", "sha256:app-older"})

	if err := state.verifyComposeGroup(group); err != nil {
		t.Fatalf("verifyComposeGroup: %v", err)
	}
	if tags := tagCalls(fake.calls); len(tags) != 0 || len(commands.restores) != 0 {
		t.Fatalf("tags %v, restores %v; want no rollback", tags, commands.restores)
	}
	if len(state.outcomes) != 2 {
		t.Fatalf("outcomes = %+v", state.outcomes)
	}
	if err := state.outcomes[0].Err; err == nil || !strings.Contains(err.Error(), "not rolled back") || !strings.Contains(err.Error(), "different images") {
		t.Fatalf("app's error = %v, want it not rolled back for the different images", err)
	}
	if worker := state.outcomes[1]; worker.Err != nil || worker.NewImageID != "sha256:app-new" {
		t.Fatalf("worker outcome = %+v, want it updated", worker)
	}
}

// A service still coming up when its wait ended may be migrating its data:
// no service under its tag is rolled back, even when another one failed.
func TestScheduledComposeDoesNotRollBackATagWithAServiceStillComingUp(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		state, group, fake, commands := scheduledComposeFixture(t, [2]string{"example/app:latest", "example/app:latest"}, [2]string{"sha256:app-old", "sha256:app-old"})
		starting := container.InspectResponse{ID: "worker-new", Name: "/worker", Image: "sha256:app-new", State: &container.State{Status: container.StateRunning, Running: true, Health: &container.Health{Status: "starting"}}, Config: &container.Config{Image: "example/app:latest"}}
		fake.inspectResults["worker"], fake.inspectResults["worker-new"] = starting, starting

		if err := state.verifyComposeGroup(group); err != nil {
			t.Fatalf("verifyComposeGroup: %v", err)
		}
		if tags := tagCalls(fake.calls); len(tags) != 0 || len(commands.restores) != 0 {
			t.Fatalf("tags %v, restores %v; want no rollback", tags, commands.restores)
		}
		if len(state.outcomes) != 2 {
			t.Fatalf("outcomes = %+v, want both", state.outcomes)
		}
		// app failed and is held back by worker; worker says why of itself.
		for i, want := range []string{"worker under the same tag had not failed", "had not exited"} {
			if err := state.outcomes[i].Err; err == nil || errors.As(err, new(rolledBackError)) || !strings.Contains(err.Error(), want) {
				t.Fatalf("outcome %d = %v, want failed, not rolled back, saying %q", i, err, want)
			}
		}
	})
}

// A wait that ended without a verdict on the new container (paused, a state
// that cannot be read, the container not found) is not rolled back, and says
// so rather than claiming the container was still coming up.
func TestComposeUpdateWithoutAVerdictIsNotRolledBack(t *testing.T) {
	paused := &container.State{Status: container.StatePaused, Running: true, Paused: true}
	t.Run("on demand", func(t *testing.T) {
		withTempUpdateStatusPath(t)
		before, fake, restored := composeUpdateFixture()
		pausedWeb := container.InspectResponse{ID: "new-web", Name: "/web", Image: "sha256:new", State: paused}
		fake.inspectResults["web"], fake.inspectResults["new-web"] = pausedWeb, pausedWeb
		commands := &composeRollbackFake{}
		rollback := commands.rollback(fake, map[string]composeServiceSettings{"web": {Image: "nginx:latest"}}, restored)

		_, err := updateComposeContainerWithRunner(t.Context(), fake, rollback, before, composeTestTarget, "web", updateResultFor(before), pulledAndUp, nil)
		if err == nil || !strings.Contains(err.Error(), "container is paused") || !strings.Contains(err.Error(), "no verdict") || strings.Contains(err.Error(), "had not exited") {
			t.Fatalf("error = %v, want it not rolled back for the missing verdict", err)
		}
		if tags := tagCalls(fake.calls); len(tags) != 0 {
			t.Fatalf("tags = %v, want none", tags)
		}
	})
	t.Run("in a pass", func(t *testing.T) {
		state, group, fake, _ := scheduledComposeFixture(t, [2]string{"example/app:latest", "example/app:latest"}, [2]string{"sha256:app-old", "sha256:app-old"})
		pausedApp := container.InspectResponse{ID: "app-new", Name: "/app", Image: "sha256:app-new", State: paused, Config: &container.Config{Image: "example/app:latest"}}
		fake.inspectResults["app"], fake.inspectResults["app-new"] = pausedApp, pausedApp

		if err := state.verifyComposeGroup(group); err != nil {
			t.Fatalf("verifyComposeGroup: %v", err)
		}
		if err := state.outcomes[0].Err; err == nil || !strings.Contains(err.Error(), "no verdict") || strings.Contains(err.Error(), "had not exited") {
			t.Fatalf("app's error = %v, want it not rolled back for the missing verdict", err)
		}
	})
}

// A tag read or move that runs out of its own time says so: a bare deadline
// error reads as "operation timed out" alone in the bridge's task runner.
func TestComposeRollbackTagThatTimesOutSaysSo(t *testing.T) {
	for name, block := range map[string]func(*fakeNativeUpdateClient){
		"the read": func(fake *fakeNativeUpdateClient) { fake.imageInspectBlocks = true },
		"the move": func(fake *fakeNativeUpdateClient) { fake.tagBlocks = true },
	} {
		t.Run(name, func(t *testing.T) {
			withTempUpdateStatusPath(t)
			synctest.Test(t, func(t *testing.T) {
				before, fake, restored := composeUpdateFixture()
				block(fake)
				commands := &composeRollbackFake{}
				rollback := commands.rollback(fake, map[string]composeServiceSettings{"web": {Image: "nginx:latest"}}, restored)

				_, err := updateComposeContainerWithRunner(t.Context(), fake, rollback, before, composeTestTarget, "web", updateResultFor(before), pulledAndUp, nil)
				if err == nil || errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "no answer within 30s") {
					t.Fatalf("error = %v, want the tag's own time named and no deadline error wrapped", err)
				}
			})
		})
	}
}
