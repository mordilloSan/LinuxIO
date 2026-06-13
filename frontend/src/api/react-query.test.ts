import { describe, expect, it, vi } from "vitest";

import { createEndpoint } from "@/api/react-query";
import * as core from "@/api/linuxio-core";

describe("createEndpoint", () => {
  it("builds deterministic query keys for no-request, field, and object shapes", () => {
    expect(
      createEndpoint("system", "get_cpu_info", { kind: "none" }).queryKey(),
    ).toEqual(["linuxio", "system", "get_cpu_info"]);

    expect(
      createEndpoint("jobs", "get", {
        kind: "field",
        field: "jobId",
      }).queryKey("job-1"),
    ).toEqual(["linuxio", "jobs", "get", { jobId: "job-1" }]);

    expect(
      createEndpoint("config", "set", { kind: "object" }).queryKey({
        theme: "dark",
      }),
    ).toEqual(["linuxio", "config", "set", { theme: "dark" }]);
  });

  it("shapes direct requests and chooses retry policy", async () => {
    const request = vi.spyOn(core, "request").mockResolvedValue("ok");

    await expect(
      createEndpoint<string>("system", "get_cpu_info", { kind: "none" })(),
    ).resolves.toBe("ok");
    expect(request).toHaveBeenLastCalledWith(
      "system",
      "get_cpu_info",
      {},
      { retryPolicy: "connection_closed" },
    );

    await createEndpoint("docker", "start_container", {
      kind: "field",
      field: "containerId",
    })("abc");
    expect(request).toHaveBeenLastCalledWith(
      "docker",
      "start_container",
      { containerId: "abc" },
      { retryPolicy: "none" },
    );
  });

  it("builds query options and rejects invalid route modes", async () => {
    const request = vi.spyOn(core, "request").mockResolvedValue({ ok: true });
    const endpoint = createEndpoint("jobs", "get", {
      kind: "field",
      field: "jobId",
    });

    const options = endpoint.queryOptions("job-1", { staleTime: 123 });
    expect(options.queryKey).toEqual([
      "linuxio",
      "jobs",
      "get",
      { jobId: "job-1" },
    ]);
    expect(options.staleTime).toBe(123);
    if (typeof options.queryFn !== "function") {
      throw new Error("Expected queryFn to be callable");
    }
    await expect(options.queryFn({} as never)).resolves.toEqual({ ok: true });
    expect(request).toHaveBeenLastCalledWith(
      "jobs",
      "get",
      { jobId: "job-1" },
      { retryPolicy: "none" },
    );

    expect(() =>
      createEndpoint("docker", "start_container", {
        kind: "field",
        field: "containerId",
      }).queryOptions("abc"),
    ).toThrow(/not query/);

    expect(() =>
      createEndpoint("system", "get_cpu_info", { kind: "none" }).useMutation(),
    ).toThrow(/not mutation\/job/);
  });
});
