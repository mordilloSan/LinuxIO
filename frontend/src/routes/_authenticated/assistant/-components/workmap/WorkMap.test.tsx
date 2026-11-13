import { describe, expect, it } from "vitest";

import { render, screen, within } from "@/test/render";

import type { TranscriptBlock } from "../transcript/transcript";
import WorkMap from "./WorkMap";
import { summarize } from "./workMap";

const blocks: TranscriptBlock[] = [
  { kind: "user", id: "u1", text: "Check the disks", attachments: [] },
  {
    kind: "tool",
    id: "t1",
    toolCallId: "c1",
    title: "Read fstab",
    toolKind: "read",
    status: "completed",
    content: [],
    opensStep: true,
    startedAt: 1000,
    endedAt: 1113,
  },
  {
    kind: "tool",
    id: "t2",
    toolCallId: "c2",
    title: "Restart nginx",
    toolKind: "execute",
    status: "failed",
    content: [],
    opensStep: true,
    startedAt: 2000,
    endedAt: 3400,
  },
];

const draw = (over: Partial<Parameters<typeof WorkMap>[0]> = {}) =>
  render(
    <WorkMap
      agentLabel="Claude Code"
      model={null}
      running={false}
      summary={summarize(blocks, false)}
      usage={null}
      {...over}
    />,
  );

const lines = () =>
  screen.getAllByRole("listitem").map((row) => row.textContent);

describe("WorkMap", () => {
  it("names the picture by where the work went", () => {
    draw();
    expect(
      screen.getByRole("img", {
        name: "Activity map. You 1, Model 2, Files 1, Shell 1.",
      }),
    ).toBeInTheDocument();
  });

  it("draws every station and counts the used ones", () => {
    draw();
    const picture = screen.getByRole("img");
    for (const label of ["You", "Model", "Memory", "Files", "Shell", "Hooks"]) {
      expect(within(picture).getByText(label)).toBeInTheDocument();
    }
    expect(within(picture).getByText("Shell: 1, 1 failed")).toBeInTheDocument();
    expect(within(picture).getByText("Agents: 0")).toBeInTheDocument();
  });

  it("shows the agent, its model and how full the context window is", () => {
    draw({ model: "Opus 5.5", usage: { used: 90_000, size: 200_000 } });
    const picture = screen.getByRole("img");
    expect(within(picture).getByText("CLAUDE")).toBeInTheDocument();
    expect(within(picture).getByText("opus 5.5")).toBeInTheDocument();
    expect(within(picture).getByText("45%")).toBeInTheDocument();
    expect(
      screen.getByText("Nothing is running. · context 45%"),
    ).toBeInTheDocument();
  });

  it("leaves the context out until the agent reports it", () => {
    draw();
    expect(screen.queryByText(/context/)).toBeNull();
    expect(screen.getByText("Nothing is running.")).toBeInTheDocument();
  });

  it("lists what happened, newest first, with how long it took", () => {
    draw();
    expect(lines()).toEqual([
      "✕ Shell    Restart nginx  1.4 s",
      // From the first call's return to the second call.
      "· Model    step 2  887 ms",
      "· Files    Read fstab  113 ms",
      "· Model    step 1",
      "· You      new message",
    ]);
  });

  it("says what is running", () => {
    const open = [
      blocks[0],
      { ...blocks[1], status: "in_progress", endedAt: undefined },
    ] as TranscriptBlock[];
    draw({ running: true, summary: summarize(open, true) });
    expect(screen.getByText("Files: Read fstab")).toBeInTheDocument();
    expect(lines()[0]).toBe("● Files    Read fstab  running");
  });

  it("says so when nothing has happened", () => {
    draw({ summary: summarize([], false) });
    expect(screen.getByText("Nothing yet.")).toBeInTheDocument();
    expect(
      screen.getByRole("img", { name: "Activity map. Nothing yet." }),
    ).toBeInTheDocument();
  });

  it("rings a station while one of its calls runs, and counts several", () => {
    const one = { ...blocks[1], status: "in_progress" } as TranscriptBlock;
    const two = { ...one, id: "t9", toolCallId: "c9" } as TranscriptBlock;
    const { container, rerender } = draw({ summary: summarize([one], false) });
    expect(container.querySelector(".work-map__beat")).toBeNull();
    rerender(
      <WorkMap
        agentLabel="Claude Code"
        model={null}
        running
        summary={summarize([one, two], true)}
        usage={null}
      />,
    );
    expect(container.querySelectorAll(".work-map__beat")).toHaveLength(1);
    expect(screen.getByText("×2")).toBeInTheDocument();
  });

  it("marks what went wrong on its station", () => {
    const { container } = draw();
    expect(container.querySelectorAll(".work-map__fault")).toHaveLength(1);
  });

  it("draws the route between the stations the work went through", () => {
    const { container } = draw();
    // Files to Shell: the prompt was not seen, so it is not a stop.
    expect(container.querySelectorAll(".work-map__leg")).toHaveLength(1);
    expect(
      container.querySelectorAll(".work-map__leg-line--head"),
    ).toHaveLength(1);
  });
});
