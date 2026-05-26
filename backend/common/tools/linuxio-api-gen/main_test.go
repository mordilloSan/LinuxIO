package main

import (
	"strings"
	"testing"
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
	if !strings.Contains(out, `get_cpu_info: createEndpoint("system", "get_cpu_info")`) {
		t.Fatal("generated client.ts is missing a representative query endpoint")
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
	out := renderTypes(nil)
	for _, expected := range []string{
		"get_cpu_info: { args: []; result: CPUInfoResponse };",
		"start_container: { args: [containerId: string]; result: void };",
		"list_containers: { args: []; result: ContainerInfo[] };",
		"jobs: {",
		"list: { args: [status?: string]; result: JobSnapshot[] };",
		"compose: { args: [action: string, projectName: string, composePath?: string]; result: JobSnapshot };",
		"create_samba_share: { args: [name: string, properties: Record<string, string>]; result: { success: boolean; name: string } };",
		"set_auto_update: { args: [payload: { container: string; enabled: boolean }]; result: { message: string } };",
		"set_ntp_servers: { args: string[]; result: void };",
		`validate_compose: { args: [content: string]; result: { valid: boolean; errors: { line?: number; column?: number; field?: string; message: string; type: "error" | "warning"; }[]; normalized_content?: string; } };`,
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
