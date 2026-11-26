package dockerupdate

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"
)

// joinedContainer is a container of service in project media (no project
// when service is empty) that joins the network namespace of the container
// with the ID joins.
func joinedContainer(id, service, joins string, running bool) container.InspectResponse {
	inspect := standaloneTestInspect()
	inspect.ID, inspect.Name = id, "/"+id
	inspect.State = &container.State{Running: running}
	if service != "" {
		inspect.Config.Labels = map[string]string{"com.docker.compose.project": "media", "com.docker.compose.service": service}
	}
	if joins != "" {
		inspect.HostConfig.NetworkMode = container.NetworkMode("container:" + joins)
	}
	return inspect
}

func composeJoinsFake(containers ...container.InspectResponse) *fakeNativeUpdateClient {
	fake := newStandaloneUpdateFake()
	for _, inspect := range containers {
		fake.containerItems = append(fake.containerItems, container.Summary{ID: inspect.ID, Names: []string{inspect.Name}})
		fake.inspectResults[inspect.ID] = inspect
	}
	return fake
}

func TestComposeServiceJoinsNamesTheRunningServicesThatJoinOne(t *testing.T) {
	ipc := joinedContainer("ipc-id", "ipc", "", true)
	ipc.HostConfig.IpcMode = "container:vpn-id"
	job := joinedContainer("job-id", "app", "", true)
	job.Config.Labels["com.docker.compose.oneoff"] = "True"
	fake := composeJoinsFake(
		joinedContainer("vpn-id", "vpn", "", true),
		joinedContainer("app-id", "app", "vpn-id", true),
		ipc,
		joinedContainer("idle-id", "idle", "vpn-id", false),
		joinedContainer("db-id", "db", "", true),
		joinedContainer("other-id", "", "db-id", true),
		job,
	)

	joins, err := composeServiceJoins(context.Background(), fake, "media", []string{"vpn"})
	if err != nil {
		t.Fatalf("composeServiceJoins: %v", err)
	}
	// The stopped service is left alone, and what joins a service that is
	// not updated does not count.
	if got := joins.of([]string{"vpn"}); !reflect.DeepEqual(got, []string{"app", "ipc"}) {
		t.Fatalf("dependents of vpn = %v, want app and ipc", got)
	}
	if got := joins.of([]string{"vpn", "app"}); !reflect.DeepEqual(got, []string{"ipc"}) {
		t.Fatalf("dependents of vpn and app = %v, want ipc alone", got)
	}
}

func TestComposeServiceJoinsRefusesAContainerOutsideTheProject(t *testing.T) {
	job := joinedContainer("job-id", "app", "vpn-id", true)
	job.Config.Labels["com.docker.compose.oneoff"] = "True"
	for name, outside := range map[string]container.InspectResponse{
		"standalone":  joinedContainer("other-id", "", "vpn-id", false),
		"compose run": job,
	} {
		t.Run(name, func(t *testing.T) {
			fake := composeJoinsFake(joinedContainer("vpn-id", "vpn", "", true), outside)
			_, err := composeServiceJoins(context.Background(), fake, "media", []string{"vpn"})
			if err == nil || !strings.Contains(err.Error(), `provides the network namespace used by container "`+outside.ID+`"`) {
				t.Fatalf("composeServiceJoins error = %v, want a refusal naming %s", err, outside.ID)
			}
		})
	}
}

// The update's `up` and the rollback's both name the service that joins the
// updated one.
func TestUpdateComposeContainerRecreatesWhatJoinsTheService(t *testing.T) {
	withTempUpdateStatusPath(t)
	before, fake, restored := composeUpdateFixture()
	before.Config.Labels = map[string]string{"com.docker.compose.project": "media", "com.docker.compose.service": "web"}
	app := joinedContainer("app-id", "app", before.ID, true)
	fake.containerItems = []container.Summary{{ID: before.ID, Names: []string{before.Name}}, {ID: app.ID, Names: []string{app.Name}}}
	fake.inspectResults[before.ID], fake.inspectResults[app.ID] = before, app
	commands := &composeRollbackFake{}
	rollback := commands.rollback(fake, map[string]composeServiceSettings{"web": {Image: "nginx:latest"}}, restored)

	var broughtUp []string
	run := func(_ context.Context, _ ComposeTarget, service string, dependents []string, _ ComposeLineEmitter) error {
		broughtUp = append([]string{service}, dependents...)
		return nil
	}
	_, err := updateComposeContainerWithRunner(t.Context(), fake, rollback, before, composeTestTarget, "web", updateResultFor(before), run, nil)
	if err == nil {
		t.Fatal("the update succeeded; the fixture's new container fails")
	}
	if want := []string{"web", "app"}; !reflect.DeepEqual(broughtUp, want) || !reflect.DeepEqual(commands.restores, [][]string{want}) {
		t.Fatalf("update brought up %v, rollback %v; want %v from both", broughtUp, commands.restores, want)
	}
	// The rollback's checks are of the service it rolls back alone.
	if !reflect.DeepEqual(commands.settingsFor, [][]string{{"web"}}) {
		t.Fatalf("settings read for %v, want web alone", commands.settingsFor)
	}
}
