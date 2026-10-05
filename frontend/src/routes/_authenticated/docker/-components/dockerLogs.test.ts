import { describe, expect, it } from "vitest";

import {
  dockerLogHue,
  formatDockerLogTime,
  mergeDockerLogEntries,
  parseDockerLogFrame,
  stageDockerLogEntries,
  type DockerLogEntry,
} from "./dockerLogs";

const entry = (
  seq: number,
  ts: string,
  name = "web",
  line = `line ${seq}`,
): DockerLogEntry => ({
  id: "abc123def456",
  line,
  name,
  seq,
  stderr: false,
  ts,
});

describe("parseDockerLogFrame", () => {
  it("parses NDJSON records and skips malformed lines", () => {
    const text =
      '{"id":"abc123def456","name":"web","ts":"2026-10-05T10:00:00.000000001Z","line":"hello"}\n' +
      "not json\n" +
      '{"id":"abc123def456","name":"web","ts":"2026-10-05T10:00:00.000000002Z","line":"oops","stderr":true}\n';
    expect(parseDockerLogFrame(text, 7)).toEqual([
      entry(7, "2026-10-05T10:00:00.000000001Z", "web", "hello"),
      {
        ...entry(8, "2026-10-05T10:00:00.000000002Z", "web", "oops"),
        stderr: true,
      },
    ]);
  });
});

describe("mergeDockerLogEntries", () => {
  it("sorts an interleaved backlog by timestamp", () => {
    const current = [entry(0, "2026-10-05T10:00:05Z", "web")];
    const batch = [
      entry(1, "2026-10-05T10:00:03Z", "db"),
      entry(2, "2026-10-05T10:00:01Z", "db"),
      entry(3, "2026-10-05T10:00:06Z", "web"),
    ];
    expect(mergeDockerLogEntries(current, batch).map((e) => e.seq)).toEqual([
      2, 1, 0, 3,
    ]);
  });

  it("appends a live batch and drops the oldest past the cap", () => {
    const current = [
      entry(0, "2026-10-05T10:00:01Z"),
      entry(1, "2026-10-05T10:00:02Z"),
    ];
    const batch = [entry(2, "2026-10-05T10:00:03Z")];
    expect(mergeDockerLogEntries(current, batch, 2).map((e) => e.seq)).toEqual([
      1, 2,
    ]);
  });

  it("keeps arrival order for entries without a timestamp", () => {
    const current = [entry(0, "2026-10-05T10:00:05Z")];
    const batch = [entry(1, ""), entry(2, "")];
    expect(mergeDockerLogEntries(current, batch).map((e) => e.seq)).toEqual([
      0, 1, 2,
    ]);
  });
});

describe("dockerLogHue", () => {
  it("is stable per name and within the hue circle", () => {
    expect(dockerLogHue("web")).toBe(dockerLogHue("web"));
    expect(dockerLogHue("web")).not.toBe(dockerLogHue("db"));
    for (const name of ["web", "db", "cache", "a", ""]) {
      const hue = dockerLogHue(name);
      expect(hue).toBeGreaterThanOrEqual(0);
      expect(hue).toBeLessThan(360);
    }
  });
});

describe("formatDockerLogTime", () => {
  it("formats a valid timestamp as a clock time and passes junk through", () => {
    expect(formatDockerLogTime("2026-10-05T10:00:00.000000001Z")).toMatch(
      /^\d{2}:\d{2}:\d{2}$/,
    );
    expect(formatDockerLogTime("")).toBe("");
    expect(formatDockerLogTime("junk")).toBe("junk");
  });
});

describe("stageDockerLogEntries", () => {
  it("keeps only the newest entries while flushing is paused", () => {
    const pending = [entry(0, "2026-10-05T10:00:00Z")];
    stageDockerLogEntries(
      pending,
      [
        entry(1, "2026-10-05T10:00:01Z"),
        entry(2, "2026-10-05T10:00:02Z"),
        entry(3, "2026-10-05T10:00:03Z"),
      ],
      3,
    );
    expect(pending.map((e) => e.seq)).toEqual([1, 2, 3]);
  });
});
