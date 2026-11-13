import { describe, expect, it } from "vitest";

import type { ToolBlock, TranscriptBlock } from "../transcript/transcript";
import {
  formatMs,
  labelOf,
  rowLine,
  rowTime,
  shellStation,
  stationOf,
  summarize,
  type WorkRow,
} from "./workMap";

let next = 0;
const tool = (over: Partial<ToolBlock> = {}): ToolBlock => ({
  kind: "tool",
  id: `t${++next}`,
  toolCallId: `call${next}`,
  title: "A call",
  toolKind: "other",
  status: "completed",
  content: [],
  ...over,
});

describe("shellStation", () => {
  it.each([
    ["cat /etc/hosts | grep local", "files"],
    ["cd /srv && ls -la", "files"],
    ["curl -s https://example.com | jq .name", "network"],
    ["sudo systemctl restart nginx", "shell"],
    ["FOO=1 make test", "shell"],
    ["echo 'curl; wget' > notes", "shell"],
    ['git commit -m "ssh keys; curl"', "shell"],
    ["echo hello", "shell"],
  ])("sorts %s to %s", (command, station) => {
    expect(shellStation(command)).toBe(station);
  });

  it("keeps a call without a command line at the shell", () => {
    expect(shellStation(null)).toBe("shell");
  });
});

describe("stationOf", () => {
  it("goes by the agent's tool name before the kind", () => {
    expect(stationOf(tool({ toolName: "Skill", toolKind: "other" }))).toBe(
      "skills",
    );
    expect(stationOf(tool({ toolName: "Agent", toolKind: "think" }))).toBe(
      "agents",
    );
    expect(stationOf(tool({ toolName: "WebSearch", toolKind: "fetch" }))).toBe(
      "network",
    );
    expect(stationOf(tool({ toolName: "AskUserQuestion" }))).toBe("you");
    expect(stationOf(tool({ toolName: "memory_recall" }))).toBe("memory");
    expect(stationOf(tool({ toolName: "TodoWrite" }))).toBe("shell");
  });

  it("sorts a shell tool by its command line", () => {
    const bash = (command: string) =>
      tool({ toolName: "Bash", toolKind: "execute", rawInput: { command } });
    expect(stationOf(bash("rg -n TODO src"))).toBe("files");
    expect(stationOf(bash("ssh host uptime"))).toBe("network");
    expect(stationOf(bash("make build"))).toBe("shell");
  });

  it("sends the agent's own notes to memory", () => {
    const read = (file_path: string) =>
      tool({ toolName: "Read", toolKind: "read", rawInput: { file_path } });
    expect(stationOf(read("/home/u/.claude/projects/p/memory/note.md"))).toBe(
      "memory",
    );
    expect(stationOf(read("/srv/app/main.go"))).toBe("files");
  });

  it("sorts MCP tools by their server", () => {
    expect(stationOf(tool({ toolName: "mcp__Claude_Browser__click" }))).toBe(
      "browser",
    );
    expect(stationOf(tool({ toolName: "mcp__memory__search" }))).toBe("memory");
    expect(stationOf(tool({ toolName: "mcp__terminal__run" }))).toBe("shell");
    expect(stationOf(tool({ toolName: "mcp__github__list_issues" }))).toBe(
      "network",
    );
  });

  it("falls back to the kind for an agent that sends no tool name", () => {
    expect(stationOf(tool({ toolKind: "edit" }))).toBe("files");
    expect(stationOf(tool({ toolKind: "fetch" }))).toBe("network");
    expect(stationOf(tool({ toolKind: "think" }))).toBe("model");
    expect(
      stationOf(tool({ toolKind: "execute", rawInput: { command: "ls" } })),
    ).toBe("files");
    expect(stationOf(tool({ toolKind: "other" }))).toBe("shell");
  });
});

describe("labelOf", () => {
  it("names a call in a few words, never by its whole input", () => {
    expect(
      labelOf(
        tool({
          toolName: "Bash",
          rawInput: { command: "lsblk -d -o NAME", description: "List disks" },
        }),
      ),
    ).toBe("List disks");
    expect(
      labelOf(tool({ toolName: "Bash", rawInput: { command: "lsblk -d" } })),
    ).toBe("lsblk");
    expect(
      labelOf(
        tool({ toolName: "Read", rawInput: { file_path: "/srv/app/main.go" } }),
      ),
    ).toBe("app/main.go");
    expect(
      labelOf(
        tool({
          toolName: "WebFetch",
          rawInput: { url: "https://user:pw@example.com/a?b=c" },
        }),
      ),
    ).toBe("example.com");
    expect(labelOf(tool({ toolName: "WebSearch" }))).toBe("web search");
    expect(labelOf(tool({ toolName: "Agent" }))).toBe("subagent");
    expect(
      labelOf(tool({ toolName: "Skill", rawInput: { skill: "dataviz" } })),
    ).toBe("dataviz");
    expect(labelOf(tool({ toolName: "mcp__github__list_issues" }))).toBe(
      "github: list issues",
    );
    expect(labelOf(tool({ toolName: "mcp__Claude_Browser__take_shot" }))).toBe(
      "take shot",
    );
  });

  it("uses the agent's words for a call without a tool name", () => {
    expect(labelOf(tool({ title: "df -h" }))).toBe("df -h");
    expect(
      labelOf(tool({ title: "df -h", rawInput: { description: "Disk use" } })),
    ).toBe("Disk use");
  });
});

describe("formatMs", () => {
  it.each([
    [0.2, "1 ms"],
    [113, "113 ms"],
    [13_400, "13.4 s"],
    [125_000, "2 min 5 s"],
  ])("writes %d as %s", (ms, text) => {
    expect(formatMs(ms)).toBe(text);
  });
});

describe("rowLine and rowTime", () => {
  const row = (over: Partial<WorkRow> = {}): WorkRow => ({
    id: "r1",
    station: "files",
    label: "disks/lsblk.go",
    status: "ok",
    times: 1,
    tookMs: 1200,
    ...over,
  });

  it("writes the row's words apart from its time", () => {
    expect(rowLine(row({ times: 3 }))).toBe("· Files    disks/lsblk.go  +2");
    expect(rowTime(row())).toBe("1.2 s");
  });

  it.each([
    ["running", row({ status: "run", tookMs: null }), "running"],
    ["a moment", row({ tookMs: 0 }), "—"],
    ["a time not seen", row({ tookMs: null }), ""],
  ])("gives %s its time", (_name, given, time) => {
    expect(rowTime(given)).toBe(time);
  });
});

describe("summarize", () => {
  // One turn seen happening: a prompt, a step with two calls side by side, a
  // step with a call that failed, and the answer.
  const turn: TranscriptBlock[] = [
    {
      kind: "user",
      id: "u1",
      text: "Check the disks\nplease",
      attachments: [],
      at: 1000,
      model: "Opus 5.5",
    },
    { kind: "thought", id: "th1", text: "Looking" },
    tool({
      toolKind: "execute",
      title: "lsblk -d",
      rawInput: { command: "lsblk -d", description: "List disks" },
      startedAt: 3000,
      endedAt: 3100,
    }),
    tool({
      toolKind: "execute",
      title: "smartctl -a /dev/sda",
      rawInput: { command: "smartctl -a /dev/sda", description: "Query SMART" },
      startedAt: 3010,
      endedAt: 3500,
    }),
    tool({
      toolKind: "fetch",
      title: "Fetch docs",
      status: "failed",
      opensStep: true,
      startedAt: 5000,
      endedAt: 5200,
    }),
    { kind: "agent", id: "a1", text: "Done" },
    { kind: "turn", id: "e1", durationMs: 8000, usage: null, at: 9000 },
  ];

  it("counts what happened and what was seen happening", () => {
    const { tally } = summarize(turn, false);
    expect(tally.you).toMatchObject({ count: 1, seen: { out: 1, back: 1 } });
    expect(tally.model).toMatchObject({ count: 3, seen: { out: 3, back: 3 } });
    expect(tally.shell).toMatchObject({
      count: 2,
      open: 0,
      seen: { out: 2, back: 2, failed: 0 },
    });
    expect(tally.network).toMatchObject({
      count: 1,
      faults: 1,
      seen: { out: 1, back: 1, failed: 1 },
    });
    expect(tally.agents.count).toBe(0);
  });

  it("lists prompts, steps and calls, newest first, with how long each took", () => {
    expect(
      summarize(turn, false).rows.map((row) => [
        row.station,
        row.label,
        row.status,
        row.times,
        row.tookMs,
      ]),
    ).toEqual([
      // The whole turn, from the prompt being sent.
      ["you", "answer ready", "ok", 1, 8000],
      // From the failed call's return to the end of the turn.
      ["model", "opus 5.5 · step 3", "ok", 1, 3800],
      ["network", "Fetch docs", "err", 1, 200],
      // From both shell calls being back to the next call.
      ["model", "opus 5.5 · step 2", "ok", 1, 1500],
      // Two calls in a row at one station: the newest, and their time added.
      ["shell", "Query SMART", "ok", 2, 590],
      // From the prompt to the first call.
      ["model", "opus 5.5 · step 1", "ok", 1, 2000],
      // A message arriving takes no time.
      ["you", "new message", "ok", 1, 0],
    ]);
  });

  it("traces the route the work took, without the model", () => {
    expect(summarize(turn, false).route).toEqual([
      { station: "you", at: 1000 },
      // Two calls in a row at one station are one stop, at the later one.
      { station: "shell", at: 3010 },
      { station: "network", at: 5000 },
      { station: "you", at: 9000 },
    ]);
  });

  it("times and animates nothing in a chat that was loaded", () => {
    const loaded = turn.map((block) => ({
      ...block,
      at: undefined,
      startedAt: undefined,
      endedAt: undefined,
    })) as TranscriptBlock[];
    const { rows, route, tally } = summarize(loaded, false);
    // Only the message, which takes no time, has one.
    expect(rows.map((row) => row.tookMs)).toEqual([
      null,
      null,
      null,
      null,
      null,
      null,
      0,
    ]);
    expect(route).toEqual([]);
    expect(tally.shell).toMatchObject({
      count: 2,
      seen: { out: 0, back: 0, failed: 0 },
    });
    expect(tally.model.seen).toEqual({ out: 0, back: 0, failed: 0 });
  });

  it("notes a change of model between prompts", () => {
    const blocks: TranscriptBlock[] = [
      {
        kind: "user",
        id: "u1",
        text: "a",
        attachments: [],
        model: "Fable 5.2",
      },
      { kind: "turn", id: "e1", durationMs: 1, usage: null },
      { kind: "user", id: "u2", text: "b", attachments: [], model: "Opus 5.5" },
    ];
    expect(summarize(blocks, false).rows.slice(0, 2)).toMatchObject([
      { station: "model", label: "fable 5.2 → opus 5.5", status: "switch" },
      { station: "you", label: "new message" },
    ]);
  });

  it("forgets what went wrong at the next prompt", () => {
    const blocks: TranscriptBlock[] = [
      tool({ toolKind: "fetch", status: "failed" }),
      { kind: "user", id: "u2", text: "again", attachments: [] },
    ];
    expect(summarize(blocks.slice(0, 1), false).tally.network.faults).toBe(1);
    expect(summarize(blocks, false).tally.network.faults).toBe(0);
  });

  it("says what is running while a turn runs", () => {
    const running: TranscriptBlock[] = [
      { kind: "user", id: "u1", text: "Go", attachments: [] },
      tool({ title: "First", status: "in_progress", opensStep: true }),
      tool({ title: "Second", status: "pending" }),
    ];
    const live = summarize(running, true);
    expect(live.now).toBe("Shell: Second and 1 more");
    expect(live.tally.shell.open).toBe(2);
    // Calls that are running are not folded: each has its row.
    expect(live.rows.slice(0, 2)).toMatchObject([
      { label: "Second", status: "run", times: 1 },
      { label: "First", status: "run", times: 1 },
    ]);

    // The same calls after the turn was stopped are no longer running.
    const stopped = summarize(running, false);
    expect(stopped.now).toBeNull();
    expect(stopped.tally.shell.open).toBe(0);
    expect(stopped.rows[0]).toMatchObject({ status: "ok", times: 2 });
  });

  it("has the model at work whenever none of its calls is", () => {
    const prompt: TranscriptBlock[] = [
      { kind: "user", id: "u1", text: "Go", attachments: [], at: 1 },
    ];
    // Straight after the prompt, before the model has said anything.
    expect(summarize(prompt, true)).toMatchObject({
      now: "Model: step 1",
      rows: [{ station: "model", status: "run" }, { station: "you" }],
      tally: { model: { count: 1, seen: { out: 1 } } },
    });
    expect(summarize(prompt, false).now).toBeNull();

    // And again once its call is back.
    const back = [...prompt, tool({ opensStep: true, endedAt: 5 })];
    expect(summarize(back, true)).toMatchObject({
      now: "Model: step 2",
      tally: { model: { count: 2 } },
    });
    // The step it then speaks in is that same step, not another.
    const spoke = [...back, { kind: "agent" as const, id: "a1", text: "So" }];
    expect(summarize(spoke, true).tally.model.count).toBe(2);
  });

  it("leaves a call in an earlier turn out of what is running", () => {
    const blocks: TranscriptBlock[] = [
      tool({ title: "Stuck", status: "pending" }),
      { kind: "turn", id: "e1", durationMs: 1, usage: null },
      { kind: "user", id: "u2", text: "Again", attachments: [] },
    ];
    expect(summarize(blocks, true).tally.shell.open).toBe(0);
  });

  it("keeps only the latest rows", () => {
    const many = Array.from({ length: 40 }, (_, i) =>
      tool({ title: `call ${i}`, toolKind: i % 2 ? "fetch" : "edit" }),
    );
    const { rows } = summarize(many, false);
    expect(rows).toHaveLength(16);
    expect(rows[0].label).toBe("call 39");
  });
});
