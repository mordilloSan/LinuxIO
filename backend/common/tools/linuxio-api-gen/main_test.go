package main

import (
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func TestRenderClientEmitsCallsAndSkipsChannels(t *testing.T) {
	out := renderClient()
	for _, unexpected := range []string{
		"logs.follow:",
		"general.follow:",
		"service.follow:",
		"terminal.open",
		"tasks.watch",
	} {
		if strings.Contains(out, unexpected) {
			t.Fatalf("generated client.ts contains non-endpoint route %q", unexpected)
		}
	}
	for _, expected := range []string{
		`get_cpu_info: defineCall("system.get_cpu_info")`,
		`start_container: defineCallWithRequest("docker.start_container")`,
		`list_containers: defineCall("docker.list_containers")`,
		`set_hostname: defineCallWithRequest("hostname.set_hostname")`,
	} {
		if !strings.Contains(out, expected) {
			t.Fatalf("generated client.ts is missing Call %s", expected)
		}
	}
}

func TestRenderClientEmitsRequestObjectEndpoints(t *testing.T) {
	out := renderClient()
	for _, expected := range []string{
		`import { createTaskEndpoint } from "../task-react-query";`,
		`import type { TypedAPI } from "../endpoint-types";`,
		`get_ntp_servers: defineCall("datetime.get_ntp_servers")`,
		`get_ntp_status: defineCall("datetime.get_ntp_status")`,
		`get_timezone: defineCall("datetime.get_timezone")`,
		`set_ntp: defineCallWithRequest("datetime.set_ntp")`,
		`set_ntp_servers: defineCallWithRequest("datetime.set_ntp_servers")`,
		`set_server_time: defineCallWithRequest("datetime.set_server_time")`,
		`set_timezone: defineCallWithRequest("datetime.set_timezone")`,
		`start_container: defineCallWithRequest("docker.start_container")`,
		`system_prune: defineCallWithRequest("docker.system_prune")`,
		`update_container: defineCallWithRequest("docker.update_container")`,
		`set_hostname: defineCallWithRequest("hostname.set_hostname")`,
		`compose: createTaskEndpoint("docker", "compose", { kind: "object" })`,
		`archive: createTaskEndpoint("filebrowser", "archive", { kind: "object" })`,
		`validate_compose: defineCallWithRequest("docker.validate_compose")`,
		`import { defineCall, defineCallWithRequest } from "../call-react-query";`,
	} {
		if !strings.Contains(out, expected) {
			t.Fatalf("generated client.ts missing endpoint fragment %s", expected)
		}
	}
	for _, unexpected := range []string{
		"createEndpoint(",
		"createQueryEndpoint(",
		`from "../react-query"`,
		"\n    get_cpu_info: createTaskEndpoint(",
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
		`export type RouteMode = "call" | "task" | "duplex";`,
		"export type RouteName = keyof typeof ROUTE_MODES;",
		"export type RouteModeFor<R extends string> =",
		`"terminal.open": "duplex"`,
		`"tasks.watch": "duplex"`,
		`"docker.logs.follow": "duplex"`,
		`"logs.general.follow": "duplex"`,
		`"logs.service.follow": "duplex"`,
		`"datetime.set_ntp": "call"`,
	} {
		if !strings.Contains(out, expected) {
			t.Fatalf("generated route metadata missing %s", expected)
		}
	}
}

func TestRenderTypesCoversCoreRouteShapes(t *testing.T) {
	out := renderTypes()
	for _, expected := range []string{
		`export interface LinuxIOCallSchema`,
		`"datetime.get_timezone": { request: void; result: string };`,
		`"datetime.set_timezone": { request: TimezoneRequest; result: void };`,
		`"system.get_cpu_info": { request: void; result: CPUInfoResponse };`,
		`"docker.start_container": { request: ContainerIDRequest; result: void };`,
		"export type CallRoute = keyof LinuxIOCallSchema;",
		"export type NoRequestCallRoute",
		"export type RequestCallRoute",
		"get_cpu_info: { input: []; request: void; result: CPUInfoResponse };",
		"start_container: { input: [containerId: string]; request: ContainerIDRequest; result: void };",
		"list_containers: { input: []; request: void; result: ContainerInfo[] };",
		"tasks: {",
		"list: { input: [request: TaskListRequest]; request: TaskListRequest; result: TaskSnapshot[] };",
		"compose: { input: [request: DockerComposeRequest]; request: DockerComposeRequest; result: ComposeTaskResult; progress: ComposeTaskMessage };",
		"create_samba_share: {",
		"input: [request: ShareSambaRequest]; request: ShareSambaRequest;",
		"archive: { input: [request: FileArchiveRequest]; request: FileArchiveRequest; result: FileArchiveResult; progress: FileProgress };",
		"resource_patch: { input: [request: ActionSourceDestinationRequest]; request: ActionSourceDestinationRequest; result: FileOperationResult; progress: FileProgress };",
		"create: { input: [request: VMCreateRequest]; request: VMCreateRequest; result: VirtualMachine; progress: VMCreateProgress };",
		"system_prune: {",
		"input: [request: DockerSystemPruneRequest]; request: DockerSystemPruneRequest;",
		"export interface DockerContainerUpdateResult",
		"export interface MessageResponse",
		"update_container: {",
		"input: [containerId: string];",
		"result: DockerContainerUpdateResult };",
		"set_ntp_servers: { input: [servers: string[]]; request: NTPServersRequest; result: void };",
		"validate_compose: {",
		"input: [content: string]; request: ContentRequest;",
		"export interface InstallCapabilityResult",
		"export interface TaskEvent",
		"export interface ComposeTaskMessage",
		"export interface ComposeTaskResult",
		"export interface ComposeProgress",
		"export type CommandProgress<",
	} {
		if !strings.Contains(out, expected) {
			t.Fatalf("generated types missing %s", expected)
		}
	}

	for _, unexpected := range []string{
		"terminal.open:",
		"tasks.watch:",
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

type GoldenProgress struct {
	Message string `json:"message"`
}

func TestRenderTypesFromGoContracts(t *testing.T) {
	routes := []apischema.RouteSpec{
		{
			Kind:    apischema.KindHandler,
			Route:   "golden.noop",
			Mode:    bridgeipc.ModeCall,
			Request: apischema.TypeOf[apischema.NoRequest](),
			Result:  apischema.TypeOf[apischema.NoResponse](),
		},
		{
			Kind:    apischema.KindHandler,
			Route:   "golden.scalar",
			Mode:    bridgeipc.ModeCall,
			Request: apischema.TypeOf[GoldenScalarRequest](),
			Result:  apischema.TypeOf[GoldenResponse](),
		},
		{
			Kind:    apischema.KindHandler,
			Route:   "golden.nested",
			Mode:    bridgeipc.ModeTask,
			Request: apischema.TypeOf[GoldenNestedRequest](),
			Result:  apischema.TypeOf[GoldenResponse](),
		},
		{
			Kind:     apischema.KindTaskRunner,
			Route:    "golden.runner",
			Mode:     bridgeipc.ModeTask,
			Request:  apischema.TypeOf[apischema.NTPServersRequest](),
			Result:   apischema.TypeOf[apischema.TaskSnapshot](),
			Progress: apischema.TypeOf[GoldenProgress](),
		},
		{
			Kind:       apischema.KindDuplex,
			Route:      "golden.stream",
			Mode:       bridgeipc.ModeDuplex,
			Request:    apischema.TypeOf[apischema.NoRequest](),
			Result:     apischema.TypeOf[apischema.NoResponse](),
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
		"runner: { input: [servers: string[]]; request: NTPServersRequest; result: TaskSnapshot; progress: GoldenProgress };",
		"export interface GoldenProgress",
		"export interface LinuxIOStreamSchema",
		"\"golden.stream\": void;",
		"export type StreamRouteName = keyof LinuxIOStreamSchema;",
	} {
		if !strings.Contains(out, expected) {
			t.Fatalf("generated Go-contract types missing %s\n%s", expected, out)
		}
	}

	if strings.Contains(out, "stream:") {
		t.Fatalf("generated endpoint types include duplex route:\n%s", out)
	}
}
