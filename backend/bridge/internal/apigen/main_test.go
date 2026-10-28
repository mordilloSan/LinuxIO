package main

import (
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
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
		`get_live: defineCall("monitoring.get_live")`,
		`get_processes: defineCall("monitoring.get_processes")`,
		`get_programs: defineCall("monitoring.get_programs")`,
		`list_containers: defineCall("docker.list_containers")`,
		`inspect_container: defineCallWithRequest("docker.inspect_container")`,
	} {
		if !strings.Contains(out, expected) {
			t.Fatalf("generated client.ts is missing Call %s", expected)
		}
	}
}

func TestRenderClientEmitsRequestObjectEndpoints(t *testing.T) {
	out := renderClient()
	for _, expected := range []string{
		`import type { TypedAPI } from "../endpoint-types";`,
		`get_ntp_servers: defineCall("datetime.get_ntp_servers")`,
		`get_ntp_status: defineCall("datetime.get_ntp_status")`,
		`get_timezone: defineCall("datetime.get_timezone")`,
		`set_ntp: defineMutation("datetime", "set_ntp", { kind: "field", field: "enabled" })`,
		`set_ntp_servers: defineMutation("datetime", "set_ntp_servers", { kind: "field", field: "servers" })`,
		`set_server_time: defineMutation("datetime", "set_server_time", { kind: "field", field: "isoTime" })`,
		`set_timezone: defineMutation("datetime", "set_timezone", { kind: "field", field: "timezone" })`,
		`start_container: defineMutation("docker", "start_container", { kind: "field", field: "containerId" })`,
		`system_prune: defineMutation("docker", "system_prune", { kind: "object" })`,
		`update_container: defineMutation("docker", "update_container", { kind: "field", field: "containerId" })`,
		`import { defineMutation } from "../mutation-react-query";`,
		`set_hostname: defineMutation("hostname", "set_hostname", { kind: "field", field: "hostname" })`,
		`compose: defineMutation("docker", "compose", { kind: "object" })`,
		`archive: defineMutation("filebrowser", "archive", { kind: "object" })`,
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
		"task-react-query",
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

func TestRenderClientEmitsMutationEndpoints(t *testing.T) {
	routes := []apischema.RouteSpec{
		{
			Kind:    apischema.KindHandler,
			Route:   "docker.inspect_container",
			Mode:    bridgeipc.ModeCall,
			Request: apischema.TypeOf[GoldenNestedRequest](),
			Result:  apischema.TypeOf[GoldenResponse](),
		},
		{
			Kind:     apischema.KindMutation,
			Route:    "docker.update_container",
			Mode:     bridgeipc.ModeMutation,
			Request:  apischema.TypeOf[GoldenNestedRequest](),
			Result:   apischema.TypeOf[GoldenResponse](),
			Progress: apischema.TypeOf[GoldenProgress](),
		},
	}

	out := renderClientForRoutes(routes)
	for _, expected := range []string{
		`import { defineMutation } from "../mutation-react-query";`,
		`update_container: defineMutation("docker", "update_container", { kind: "object" })`,
	} {
		if !strings.Contains(out, expected) {
			t.Fatalf("generated client.ts missing mutation fragment %s\n%s", expected, out)
		}
	}
	if strings.Contains(renderClientForRoutes(routes[:1]), "defineMutation") {
		t.Fatal("generated client.ts imports defineMutation without a mutation route")
	}

	types := renderTypesForRoutes(routes)
	expected := "update_container: { input: [request: GoldenNestedRequest]; request: GoldenNestedRequest; result: GoldenResponse; progress: TaskProgress<GoldenProgress> };"
	if !strings.Contains(types, expected) {
		t.Fatalf("generated types missing mutation schema %s\n%s", expected, types)
	}
}

func TestRenderRouteMetadataIncludesStreamOnlyRoutes(t *testing.T) {
	out := renderRouteMetadata()
	for _, expected := range []string{
		`export type RouteMode = "call" | "mutation" | "duplex";`,
		"export type RouteName = keyof typeof ROUTE_MODES;",
		"export const RETRY_SAFE_CALLS = {",
		`"system.get_cpu_info": true`,
		`"monitoring.get_live": true`,
		`"monitoring.get_processes": true`,
		`"monitoring.get_programs": true`,
		"export function isRetrySafeCall(route: string): boolean",
		"export type RouteModeFor<R extends string> =",
		`"terminal.open": "duplex"`,
		`"tasks.watch": "duplex"`,
		`"docker.logs.follow": "duplex"`,
		`"logs.general.follow": "duplex"`,
		`"logs.service.follow": "duplex"`,
		`"datetime.set_ntp": "mutation"`,
	} {
		if !strings.Contains(out, expected) {
			t.Fatalf("generated route metadata missing %s", expected)
		}
	}
	for _, unexpected := range []string{
		`"docker.start_container": true`,
		`"docker.check_updates": true`,
		`"tasks.watch": true`,
	} {
		if strings.Contains(out, unexpected) {
			t.Fatalf("generated retry-safe policy unexpectedly contains %s", unexpected)
		}
	}
}

func TestRenderTypesCoversCoreRouteShapes(t *testing.T) {
	out := renderTypes()
	for _, expected := range []string{
		`export interface LinuxIOCallSchema`,
		`"datetime.get_timezone": { request: void; result: string };`,
		"set_timezone: { input: [timezone: string]; request: TimezoneRequest; result: void };",
		`"system.get_cpu_info": { request: void; result: CPUInfoResponse };`,
		`"monitoring.get_live": { request: void; result: MonitoringLive };`,
		"export interface MonitoringLive",
		"gpus: Record<string, LiveGPU>;",
		"export interface LiveGPU",
		`"docker.inspect_container": { request: ContainerIDRequest; result: ContainerInspectInfo };`,
		"export type CallRoute = keyof LinuxIOCallSchema;",
		"export type NoRequestCallRoute",
		"export type RequestCallRoute",
		"get_cpu_info: { input: []; request: void; result: CPUInfoResponse };",
		"start_container: { input: [containerId: string]; request: ContainerIDRequest; result: void };",
		"list_containers: { input: []; request: void; result: ContainerInfo[] };",
		"tasks: {",
		"list: { input: [request: TaskListRequest]; request: TaskListRequest; result: TaskSnapshot[] };",
		"compose: { input: [request: DockerComposeRequest]; request: DockerComposeRequest; result: ComposeTaskResult; progress: TaskProgress<ComposeTaskMessage> };",
		"create_samba_share: {",
		"input: [request: ShareSambaRequest]; request: ShareSambaRequest;",
		"archive: { input: [request: FileArchiveRequest]; request: FileArchiveRequest; result: FileArchiveResult; progress: TaskProgress<FileProgress> };",
		"progress: TaskProgress<CountProgress> };",
		"resource_patch: { input: [request: ActionSourceDestinationRequest]; request: ActionSourceDestinationRequest; result: MessageResponse; progress: TaskProgress<FileProgress> };",
		"input: [request: AutoUpdateOptions]; request: AutoUpdateOptions;",
		`export type AutoUpdateBackend =`,
		`"mintupdate-automation"`,
		`export type NetworkBridgeHandoffState =`,
		`| "awaiting_confirmation"`,
		"export interface AutoUpdateOptionSupport",
		"can_configure: boolean;",
		"create: { input: [request: VMCreateRequest]; request: VMCreateRequest; result: VirtualMachine; progress: TaskProgress<VMCreateProgress> };",
		"export interface TaskProgress<TDetail = unknown>",
		"system_prune: {",
		"input: [request: DockerSystemPruneRequest]; request: DockerSystemPruneRequest;",
		"export interface DockerContainerUpdateResult",
		"export interface MessageResponse",
		"update_container: { input: [containerId: string]; request: DockerContainerUpdateRequest; result: DockerContainerUpdateResult; progress: TaskProgress<DockerContainerUpdateProgress> };",
		"export interface DockerContainerUpdateRequest",
		"export interface DockerContainerUpdateProgress",
		"set_ntp_servers: { input: [servers: string[]]; request: NTPServersRequest; result: void };",
		"validate_compose: {",
		"input: [request: ValidateComposeRequest]; request: ValidateComposeRequest;",
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
		`"system.get_memory_info"`,
		`"system.get_disk_throughput"`,
		`"system.get_uptime"`,
		`"system.get_fs_info"`,
		`"system.get_sensor_info"`,
		`"system.get_processes"`,
		`"network.get_interface_stats"`,
	} {
		if strings.Contains(out, unexpected) {
			t.Fatalf("generated endpoint types include unexpected route %s", unexpected)
		}
	}

	for _, duplicate := range []string{
		"export interface ChmodProgress",
		"export interface DeleteProgress",
		"export interface FileOperationResult",
		"export interface StorageCreateLVResult",
		"export interface UpdatesSetAutoUpdatesRequest",
	} {
		if strings.Contains(out, duplicate) {
			t.Fatalf("generated types retain duplicate contract %s", duplicate)
		}
	}
}

type GoldenScalarRequest struct {
	ID     string `json:"id"`
	Limit  int    `json:"limit,omitempty"`
	Offset int    `json:"offset,omitzero"`
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
			Kind:    apischema.KindMutation,
			Route:   "golden.nested",
			Mode:    bridgeipc.ModeMutation,
			Request: apischema.TypeOf[GoldenNestedRequest](),
			Result:  apischema.TypeOf[GoldenResponse](),
		},
		{
			Kind:     apischema.KindMutation,
			Route:    "golden.runner",
			Mode:     bridgeipc.ModeMutation,
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
		"limit?: number;",
		"offset?: number;",
		"noop: { input: []; request: void; result: void };",
		"scalar: { input: [request: GoldenScalarRequest]; request: GoldenScalarRequest; result: GoldenResponse };",
		"nested: { input: [request: GoldenNestedRequest]; request: GoldenNestedRequest; result: GoldenResponse };",
		"runner: { input: [servers: string[]]; request: NTPServersRequest; result: TaskSnapshot; progress: TaskProgress<GoldenProgress> };",
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
