package dockerupdate

import (
	"context"
	"fmt"
	"slices"

	"github.com/moby/moby/client"
)

// composeJoins holds, for a service of a Compose project, the running
// services of the project that join its container's network, IPC, PID or
// cgroup namespace (`network_mode: service:vpn` and the like). Docker records
// the joined container by its ID, so recreating the service alone leaves them
// in a namespace that is gone, with the loopback interface only, and unable
// to start again; they are recreated in the same `up` instead.
type composeJoins map[string][]string

// of is the services that join one of services and are not among them, in
// order.
func (j composeJoins) of(services []string) []string {
	var dependents []string
	for _, service := range services {
		for _, dependent := range j[service] {
			if !slices.Contains(services, dependent) && !slices.Contains(dependents, dependent) {
				dependents = append(dependents, dependent)
			}
		}
	}
	slices.Sort(dependents)
	return dependents
}

// composeServiceJoins reads from the engine what joins services of project
// (composeJoins).
func composeServiceJoins(ctx context.Context, cli ContainerClient, project string, services []string) (composeJoins, error) {
	containers, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("list containers before the update of Compose project %q: %w", project, err)
	}
	index, err := buildStandaloneDependencyIndex(ctx, cli, containers.Items)
	if err != nil {
		return nil, err
	}
	return index.composeJoins(project, services)
}

// composeJoins is what joins services of project among the index's
// containers. A container that is no service of the project (a standalone
// one, another project's, a `compose run`) cannot be recreated by the
// project's `up`, so it refuses the update, as a standalone container's
// dependent does. A stopped service of the project is left as it is: `up`
// would start it.
func (i *standaloneDependencyIndex) composeJoins(project string, services []string) (composeJoins, error) {
	joins := composeJoins{}
	for _, provider := range i.dependents {
		service := composeServiceOf(provider, project)
		if service == "" || !slices.Contains(services, service) {
			continue
		}
		for _, dependent := range i.dependents {
			if dependent.id == provider.id || dependent.hostConfig == nil {
				continue
			}
			namespace := standaloneNamespaceDependency(dependent.hostConfig, provider.id, provider.name)
			if namespace == "" {
				continue
			}
			joined := composeServiceOf(dependent, project)
			if joined == "" {
				return nil, fmt.Errorf("compose service %q provides the %s used by container %q, which is not a service of project %q and would lose it", service, namespace, dependent.name, project)
			}
			if dependent.running && !slices.Contains(joins[service], joined) {
				joins[service] = append(joins[service], joined)
			}
		}
	}
	return joins, nil
}

// composeServiceOf is the service of project that container is the container
// of, empty when it is none: `up` does not recreate what `compose run` or a
// Docker job started.
func composeServiceOf(container standaloneDependent, project string) string {
	if container.labels["com.docker.compose.project"] != project || dockerJobRun(container.labels) ||
		container.labels["com.docker.compose.oneoff"] == "True" {
		return ""
	}
	return container.labels["com.docker.compose.service"]
}
