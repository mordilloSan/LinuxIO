import { describe, expect, it } from "vitest";

import type { Process } from "@/api";

import { filterProcessRows, validateProcessSearch } from "./processData";

const processRows = [
  {
    pid: 11,
    name: "nginx",
    cmdline: ["nginx", "-g", "daemon off;"],
    username: "www-data",
    status: "sleeping",
    num_threads: 4,
    cpu_percent: 2,
    memory_percent: 1,
    memory_info: { rss: 10, vms: 20 },
    io_counters: {
      disk_read_bytes_per_second: 30,
      disk_write_bytes_per_second: 40,
    },
    container_id: "",
    container_name: "",
  },
  {
    pid: 12,
    name: "worker",
    cmdline: ["worker", "--queue", "jobs"],
    username: "root",
    status: "running",
    num_threads: 2,
    cpu_percent: 8,
    memory_percent: 3,
    memory_info: { rss: 50, vms: 80 },
    io_counters: {
      disk_read_bytes_per_second: 60,
      disk_write_bytes_per_second: 70,
    },
    container_id: "abc",
    container_name: "jobs",
  },
] satisfies Process[];

describe("process filtering", () => {
  it("matches process name, command line, and user", () => {
    expect(filterProcessRows(processRows, "NGINX")).toHaveLength(1);
    expect(filterProcessRows(processRows, "--QUEUE")).toEqual([processRows[1]]);
    expect(filterProcessRows(processRows, "www-data")).toEqual([
      processRows[0],
    ]);
  });

  it("returns all rows for an empty filter", () => {
    expect(filterProcessRows(processRows, "  ")).toBe(processRows);
  });
});

describe("process search", () => {
  it("keeps only the supported shareable search values", () => {
    expect(
      validateProcessSearch({ filter: "nginx", view: "programs" }),
    ).toEqual({ filter: "nginx", view: "programs" });
    expect(validateProcessSearch({ filter: "", view: "invalid" })).toEqual({});
  });
});
