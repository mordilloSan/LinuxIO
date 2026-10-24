import { describe, expect, it } from "vitest";

import type { Service, Socket, Timer } from "@/api";

import {
  serviceKind,
  socketKind,
  timerKind,
  unitListQueries,
} from "./unitKinds";

const service = {
  active_enter_timestamp: 0,
  active_state: "active",
  description: "Web server",
  inactive_enter_timestamp: 0,
  load_state: "loaded",
  name: "nginx.service",
  sub_state: "running",
  unit_file_state: "enabled",
} satisfies Service;

const socket = {
  ...service,
  listen: ["0.0.0.0:80"],
  n_accepted: 0,
  n_connections: 0,
  name: "nginx.socket",
} satisfies Socket;

const timer = {
  ...service,
  last_trigger_usec: 0,
  name: "backup.timer",
  next_elapse_usec: 0,
  unit: "backup.service",
} satisfies Timer;

describe("unit kinds", () => {
  it("derives each surface id from the noun", () => {
    expect(
      [serviceKind, socketKind, timerKind].map((kind) => kind.surfaceId),
    ).toEqual(["services.list", "sockets.list", "timers.list"]);
  });

  it("searches name, description and the kind's own fields", () => {
    expect(serviceKind.matchesSearch(service, "web")).toBe(true);
    expect(serviceKind.matchesSearch(service, "80")).toBe(false);
    expect(socketKind.matchesSearch(socket, "0.0.0.0")).toBe(true);
    expect(timerKind.matchesSearch(timer, "backup.service")).toBe(true);
  });

  it("builds one table cell per column on desktop and two on mobile", () => {
    expect(serviceKind.renderTableRow(service, false)).toHaveLength(
      serviceKind.desktopColumns.length,
    );
    expect(socketKind.renderTableRow(socket, false)).toHaveLength(
      socketKind.desktopColumns.length,
    );
    expect(timerKind.renderTableRow(timer, false)).toHaveLength(
      timerKind.desktopColumns.length,
    );
    expect(serviceKind.renderTableRow(service, true)).toHaveLength(2);
  });

  it("hides service PID and memory until systemd reports them", () => {
    const hidden = (info: Parameters<typeof serviceKind.cardRows>[1]) =>
      serviceKind
        .cardRows(service, info)
        .filter((row) => row.hidden)
        .map((row) => row.label);

    expect(hidden(undefined)).toEqual(["PID", "Memory"]);
    expect(hidden({ MainPID: 42, MemoryCurrent: 2048 })).toEqual([]);
    expect(hidden({ MainPID: 42, MemoryCurrent: 2 ** 64 })).toEqual(["Memory"]);
  });

  it("loads the list and, when open, the selected unit", () => {
    const list = { queryKey: ["list"] };
    const selected = { queryKey: ["unit"] };

    expect(
      unitListQueries({
        listQueryOptions: list,
        selectedQueryOptions: undefined,
      }),
    ).toEqual([list]);
    expect(
      unitListQueries({
        listQueryOptions: list,
        selectedQueryOptions: selected,
      }),
    ).toEqual([list, selected]);
  });
});
