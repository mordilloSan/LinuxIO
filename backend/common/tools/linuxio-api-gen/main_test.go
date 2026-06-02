package main

import (
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func TestRenderClientSkipsNonEndpointRoutes(t *testing.T) {
	out := renderClient()
	for _, unexpected := range []string{
		"logs.follow:",
		"general.follow:",
		"service.follow:",
		"terminal.open",
		"jobs.attach",
	} {
		if strings.Contains(out, unexpected) {
			t.Fatalf("generated client.ts contains non-endpoint route %q", unexpected)
		}
	}
	if !strings.Contains(out, `get_cpu_info: createEndpoint("system", "get_cpu_info", { kind: "none" })`) {
		t.Fatal("generated client.ts is missing a representative query endpoint")
	}
}

func TestRenderClientEmitsRequestObjectEndpoints(t *testing.T) {
	out := renderClient()
	for _, expected := range []string{
		`import { createEndpoint } from "../react-query";`,
		`start_container: createEndpoint("docker", "start_container", { kind: "field", field: "containerId" })`,
		`set_auto_update: createEndpoint("docker", "set_auto_update", { kind: "object" })`,
		`set_ntp_servers: createEndpoint("datetime", "set_ntp_servers", { kind: "field", field: "servers" })`,
		`compose: createEndpoint("docker", "compose", { kind: "object" })`,
		`archive: createEndpoint("filebrowser", "archive", { kind: "object" })`,
		`validate_compose: createEndpoint("docker", "validate_compose", { kind: "field", field: "content" })`,
	} {
		if !strings.Contains(out, expected) {
			t.Fatalf("generated client.ts missing endpoint fragment %s", expected)
		}
	}

	for _, unexpected := range []string{
		"serialize" + "StringArg",
		"serialize" + "OptionalStringArg",
		"trimTrailing" + "Undefined",
		"JSON.stringify",
	} {
		if strings.Contains(out, unexpected) {
			t.Fatalf("generated client.ts contains legacy serializer fragment %s", unexpected)
		}
	}
}

func TestRenderRouteMetadataIncludesStreamOnlyRoutes(t *testing.T) {
	out := renderRouteMetadata()
	for _, expected := range []string{
		`"terminal.open": "duplex"`,
		`"jobs.attach": "duplex"`,
		`"logs.general.follow": "job"`,
	} {
		if !strings.Contains(out, expected) {
			t.Fatalf("generated route metadata missing %s", expected)
		}
	}
}

func TestRenderTypesCoversCoreRouteShapes(t *testing.T) {
	out := renderTypes()
	for _, expected := range []string{
		"get_cpu_info: { input: []; request: void; result: CPUInfoResponse };",
		"start_container: { input: [containerId: string]; request: ContainerIDRequest; result: void };",
		"list_containers: { input: []; request: void; result: ContainerInfo[] };",
		"jobs: {",
		"list: { input: [request: JobListRequest]; request: JobListRequest; result: JobSnapshot[] };",
		"compose: { input: [request: DockerComposeRequest]; request: DockerComposeRequest; result: JobSnapshot };",
		"create_samba_share: {",
		"input: [request: ShareSambaRequest]; request: ShareSambaRequest;",
		"archive: { input: [request: FileArchiveRequest]; request: FileArchiveRequest; result: JobSnapshot };",
		"system_prune: {",
		"input: [request: DockerSystemPruneRequest]; request: DockerSystemPruneRequest;",
		"export interface DockerSetAutoUpdateRequest",
		"export interface MessageResponse",
		"set_auto_update: {",
		"input: [request: DockerSetAutoUpdateRequest]; request: DockerSetAutoUpdateRequest;",
		"set_ntp_servers: { input: [servers: string[]]; request: NTPServersRequest; result: void };",
		"validate_compose: {",
		"input: [content: string]; request: ContentRequest;",
		"export interface InstallCapabilityResult",
		"export interface JobEvent",
	} {
		if !strings.Contains(out, expected) {
			t.Fatalf("generated types missing %s", expected)
		}
	}

	for _, unexpected := range []string{
		"terminal.open:",
		"jobs.attach:",
	} {
		if strings.Contains(out, unexpected) {
			t.Fatalf("generated endpoint types include duplex route %s", unexpected)
		}
	}
}

type GoldenScalarRequest struct {
	ID    string `json:"id"`
	Limit int    `json:"limit,omitempty"`
}

type GoldenNestedRequest struct {
	Name     string              `json:"name"`
	Enabled  bool                `json:"enabled"`
	Tags     []string            `json:"tags"`
	Metadata map[string]string   `json:"metadata"`
	Child    GoldenChildContract `json:"child"`
	Note     *string             `json:"note,omitempty"`
}

type GoldenChildContract struct {
	Count int `json:"count"`
}

type GoldenResponse struct {
	OK    bool     `json:"ok"`
	Items []string `json:"items"`
}

func TestRenderTypesFromGoContracts(t *testing.T) {
	routes := []apischema.RouteSpec{
		{
			Kind:    apischema.KindHandler,
			Route:   "golden.noop",
			Mode:    bridgeipc.ModeQuery,
			Request: apischema.NoRequest(),
			Result:  apischema.NoResponse(),
		},
		{
			Kind:    apischema.KindHandler,
			Route:   "golden.scalar",
			Mode:    bridgeipc.ModeQuery,
			Request: apischema.TypeOf[GoldenScalarRequest](),
			Result:  apischema.TypeOf[GoldenResponse](),
		},
		{
			Kind:    apischema.KindHandler,
			Route:   "golden.nested",
			Mode:    bridgeipc.ModeJob,
			Request: apischema.TypeOf[GoldenNestedRequest](),
			Result:  apischema.TypeOf[GoldenResponse](),
		},
		{
			Kind:    apischema.KindRunner,
			Route:   "golden.runner",
			Mode:    bridgeipc.ModeJob,
			Request: apischema.TypeOf[apischema.NTPServersRequest](),
			Result:  apischema.TypeOf[apischema.JobSnapshot](),
		},
		{
			Kind:       apischema.KindDuplex,
			Route:      "golden.stream",
			Mode:       bridgeipc.ModeDuplex,
			Request:    apischema.NoRequest(),
			Result:     apischema.NoResponse(),
			NoEndpoint: true,
		},
	}

	out := renderTypesForRoutes(routes)
	for _, expected := range []string{
		"export interface GoldenNestedRequest",
		"metadata: Record<string, string>;",
		"child: GoldenChildContract;",
		"note?: string;",
		"noop: { input: []; request: void; result: void };",
		"scalar: { input: [request: GoldenScalarRequest]; request: GoldenScalarRequest; result: GoldenResponse };",
		"nested: { input: [request: GoldenNestedRequest]; request: GoldenNestedRequest; result: GoldenResponse };",
		"runner: { input: [servers: string[]]; request: NTPServersRequest; result: JobSnapshot };",
	} {
		if !strings.Contains(out, expected) {
			t.Fatalf("generated Go-contract types missing %s\n%s", expected, out)
		}
	}

	if strings.Contains(out, "stream:") {
		t.Fatalf("generated endpoint types include duplex route:\n%s", out)
	}
}
