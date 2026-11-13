import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import {
  announce,
  call,
  chatOf,
  FINISHED,
  pair,
  says,
  type Sent,
  stateOf,
} from "@/test/assistantSubagentFixture";

import Transcript from "./Transcript";
import type { TranscriptBlock } from "./transcript";

const run = (id: string, command: string, status: string): TranscriptBlock => ({
  kind: "tool",
  id: `b-${id}`,
  toolCallId: id,
  title: command,
  toolKind: "execute",
  status,
  rawInput: { command },
  content: [],
});

/** A command that acts on the host. */
const tool = (id: string, status = "completed") =>
  run(id, `systemctl restart ${id}`, status);

/** A command that only reads. */
const check = (id: string, status = "completed") =>
  run(id, `cat ${id}`, status);

describe("Transcript", () => {
  it("renders fenced code in a pre and the surrounding text outside it", () => {
    render(
      <Transcript
        blocks={[
          {
            kind: "agent",
            id: "1",
            text: "Before\n```sh\nls -la\n```\nAfter",
          },
        ]}
      />,
    );
    expect(screen.getByText("ls -la").closest("pre")).not.toBeNull();
    expect(screen.getByText(/Before/).closest("pre")).toBeNull();
    expect(screen.getByText(/After/).closest("pre")).toBeNull();
  });

  it("shows a sent prompt's attachments as chips that open a preview", async () => {
    render(
      <Transcript
        blocks={[
          {
            kind: "user",
            id: "1",
            text: "look",
            attachments: [
              {
                id: "a",
                kind: "image",
                name: "shot.png",
                mimeType: "image/png",
                data: "YWJj",
                previewUrl: "data:image/png;base64,YWJj",
              },
              { id: "b", kind: "text", text: "one\ntwo" },
            ],
          },
        ]}
      />,
    );
    expect(screen.queryByRole("button", { name: /Remove/ })).toBeNull();
    expect(screen.getByText("2 lines")).toBeInTheDocument();
    await userEvent.click(
      screen.getByRole("button", { name: "Open shot.png" }),
    );
    expect(
      within(screen.getByRole("dialog")).getByAltText("shot.png"),
    ).toBeInTheDocument();
  });

  it("labels the log with the label prop", () => {
    render(<Transcript blocks={[]} label="Side question" />);
    expect(
      screen.getByRole("log", { name: "Side question" }),
    ).toBeInTheDocument();
  });

  it("renders a diff view for an edit once its line is opened", async () => {
    const { container } = render(
      <Transcript
        blocks={[
          {
            kind: "tool",
            id: "t1",
            toolCallId: "t1",
            title: "Edit file",
            toolKind: "edit",
            status: "completed",
            content: [
              { type: "diff", path: "/a.txt", oldText: "x\n", newText: "y\n" },
            ],
          },
        ]}
      />,
    );
    expect(container.querySelector(".assistant-diff__add")).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: /\/a\.txt/ }));
    expect(container.querySelector(".assistant-diff__add")).toHaveTextContent(
      "+y",
    );
    expect(container.querySelector(".assistant-diff__del")).toHaveTextContent(
      "-x",
    );
  });

  it("shows a tool's input and output, clipping long output", async () => {
    const output = `\`\`\`console\n${Array.from({ length: 12 }, (_, i) => `line ${i}`).join("\n")}\n\`\`\``;
    render(
      <Transcript
        blocks={[
          {
            kind: "tool",
            id: "t1",
            toolCallId: "t1",
            title: "ls -la",
            toolKind: "execute",
            status: "completed",
            rawInput: { command: "ls -la", description: "List files" },
            content: [
              { type: "content", content: { type: "text", text: output } },
            ],
          },
        ]}
      />,
    );
    // The line shows the step; its input and output wait behind it.
    expect(screen.getByText("Read")).toBeInTheDocument();
    expect(screen.getByText("List files")).toBeInTheDocument();
    expect(screen.queryByText("IN")).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: /ls -la/ }));
    expect(screen.getByText("IN")).toBeInTheDocument();
    expect(screen.queryByText(/```/)).toBeNull();
    expect(
      screen.getByText("ls -la", { selector: ".assistant-tool__in" }),
    ).toBeInTheDocument();
    const out = screen.getByText(/line 0/);
    expect(out).toHaveClass("assistant-tool__out--clipped");
    await userEvent.click(
      screen.getByRole("button", { name: "Show all 12 lines" }),
    );
    expect(out).not.toHaveClass("assistant-tool__out--clipped");
  });

  describe("turn footer", () => {
    const turn = (usage: { outputTokens: number } | null): TranscriptBlock => ({
      kind: "turn",
      id: "t",
      durationMs: 14_200,
      usage: usage && { inputTokens: 12, totalTokens: 0, ...usage },
    });

    it("shows how long the turn took and the tokens it wrote", () => {
      render(
        <Transcript
          blocks={[
            { kind: "agent", id: "1", text: "done" },
            turn({ outputTokens: 2100 }),
          ]}
        />,
      );
      const stats = screen.getByText("14 s · 2.1k tokens");
      expect(stats).toHaveAttribute("title", "Output 2,100, input 12");
    });

    it("shows the time alone when the agent reports no usage", () => {
      render(
        <Transcript
          blocks={[{ kind: "agent", id: "1", text: "done" }, turn(null)]}
        />,
      );
      expect(screen.getByText("14 s")).toBeInTheDocument();
    });
  });

  describe("tool runs", () => {
    const read = (id: string, status = "completed") => check(id, status);

    it("lists checks as log lines, with no card", () => {
      render(<Transcript blocks={[read("a"), read("b"), read("c")]} />);
      // A command that only reads is labelled as a read, not a run.
      expect(screen.getAllByText("Read")).toHaveLength(3);
      expect(screen.queryByText("Run")).toBeNull();
      for (const id of ["a", "b", "c"]) {
        expect(screen.getByText(`cat ${id}`)).toBeInTheDocument();
      }
      expect(screen.queryByRole("group")).toBeNull();
    });

    it("hides the older lines of a long log behind a button", async () => {
      const ids = ["a", "b", "c", "d", "e", "f", "g"];
      render(<Transcript blocks={ids.map((id) => read(id))} />);
      expect(screen.queryByText("cat a")).toBeNull();
      expect(screen.queryByText("cat b")).toBeNull();
      expect(screen.getByText("cat g")).toBeInTheDocument();
      await userEvent.click(
        screen.getByRole("button", { name: "Show 2 earlier steps" }),
      );
      expect(screen.getByText("cat a")).toBeInTheDocument();
    });

    it("puts steps that acted on the host in one card", () => {
      render(<Transcript blocks={[tool("a"), tool("b")]} />);
      const card = screen.getByRole("group", { name: "Ran on this server" });
      expect(within(card).getByText("systemctl restart a")).toBeInTheDocument();
      expect(within(card).getByText("systemctl restart b")).toBeInTheDocument();
    });

    it("calls the card a change when a file was edited", () => {
      render(
        <Transcript
          blocks={[
            {
              kind: "tool",
              id: "e",
              toolCallId: "e",
              title: "Edit /etc/hosts",
              toolKind: "edit",
              status: "completed",
              content: [],
            },
          ]}
        />,
      );
      expect(
        screen.getByRole("group", { name: "Changed on this server" }),
      ).toBeInTheDocument();
    });

    it("says in words which file a file tool wrote or edited", async () => {
      const edit = (
        id: string,
        oldText: string | null,
        newText: string,
      ): TranscriptBlock => ({
        kind: "tool",
        id,
        toolCallId: id,
        title: `Write ${id}`,
        toolKind: "edit",
        status: "completed",
        rawInput: { file_path: `/home/u/${id}`, content: newText },
        content: [{ type: "diff", path: `/home/u/${id}`, oldText, newText }],
      });
      render(
        <Transcript
          blocks={[edit("new.txt", null, "a\nb\n"), edit("old.txt", "x", "y")]}
        />,
      );
      const wrote = screen.getByRole("button", { name: /new\.txt/ });
      expect(within(wrote).getByText("Wrote")).toBeInTheDocument();
      expect(within(wrote).getByText("/home/u/new.txt")).toBeInTheDocument();
      expect(within(wrote).getByText("2 lines")).toBeInTheDocument();
      const edited = screen.getByRole("button", { name: /old\.txt/ });
      expect(within(edited).getByText("Edited")).toBeInTheDocument();
      // The tool's own title is replaced, not repeated.
      expect(screen.queryByText(/Write new\.txt/)).toBeNull();
      // Opened, the diff stands alone: no raw JSON input beside it.
      await userEvent.click(wrote);
      expect(screen.queryByText("IN")).toBeNull();
      expect(screen.getByText("+a")).toBeInTheDocument();
    });

    it("says the card is running while a step is in progress", () => {
      render(<Transcript blocks={[tool("a"), tool("b", "in_progress")]} />);
      expect(
        screen.getByRole("group", { name: "Running on this server" }),
      ).toBeInTheDocument();
    });

    it("counts failed steps on the card and marks the line", () => {
      render(<Transcript blocks={[tool("a"), tool("b", "failed")]} />);
      expect(screen.getByText("1 failed")).toBeInTheDocument();
      expect(screen.getByRole("group")).toHaveClass("assistant-change--failed");
      expect(screen.getByRole("img", { name: "failed" })).toHaveAttribute(
        "title",
        "failed",
      );
    });

    it("keeps checks and actions apart, in order", () => {
      render(<Transcript blocks={[read("a"), tool("b"), read("c")]} />);
      const card = screen.getByRole("group", { name: "Ran on this server" });
      expect(within(card).queryByText("cat a")).toBeNull();
      expect(within(card).getByText("systemctl restart b")).toBeInTheDocument();
      expect(
        card.compareDocumentPosition(screen.getByText("cat c")) &
          Node.DOCUMENT_POSITION_FOLLOWING,
      ).toBeTruthy();
    });

    it("treats a command it cannot read as acting", () => {
      render(
        <Transcript
          blocks={[
            {
              kind: "tool",
              id: "x",
              toolCallId: "x",
              title: "something",
              toolKind: "execute",
              status: "completed",
              content: [],
            },
          ]}
        />,
      );
      expect(screen.getByRole("group")).toBeInTheDocument();
    });

    it("leaves out the call a permission card is asking about", () => {
      render(
        <Transcript blocks={[tool("a", "pending")]} pendingToolCallId="a" />,
      );
      expect(screen.queryByRole("group")).toBeNull();
      expect(screen.queryByText("systemctl restart a")).toBeNull();
    });

    it("keeps a line open when another step joins the run", async () => {
      const { rerender } = render(<Transcript blocks={[read("a")]} />);
      await userEvent.click(screen.getByRole("button", { name: /cat a/ }));
      rerender(<Transcript blocks={[read("a"), read("b")]} />);
      expect(screen.getByText("IN")).toBeInTheDocument();
      expect(screen.getByText("cat b")).toBeInTheDocument();
    });
  });

  describe("subagents", () => {
    // Two subagents at once, each with a call; the first is messaged again.
    const sent: Sent[] = [
      ["chat", announce("a", { title: "OS info" })],
      ...pair("p1", "chat", "a", "Read the OS release"),
      ["chat", announce("b", { title: "Host info" })],
      ...pair("p2", "chat", "b", "Read the hostname"),
      ["a", says("Reading it now.")],
      ["a", call("c1", "Bash", { command: "cat /etc/os-release" })],
      ["b", call("c2", "Bash", { command: "cat /etc/hostname" })],
      ...pair("h1", "a", "chat", "It is **Debian**."),
      ["chat", stateOf("a", FINISHED)],
      ...pair("p3", "chat", "a", "Run uptime"),
      ["a", call("c3", "Bash", { command: "uptime" })],
      ...pair("h3", "a", "chat", "Up 3 days."),
    ];
    const draw = (list: Sent[] = sent, pendingToolCallId?: string) => {
      const { chat, subagents } = chatOf(list);
      const onAgent = vi.fn();
      render(
        <Transcript
          blocks={chat.blocks}
          onAgent={onAgent}
          pendingToolCallId={pendingToolCallId}
          subagents={subagents}
        />,
      );
      return onAgent;
    };
    /** The message rows, each as its head and the commands under it. */
    const rows = () =>
      [...document.querySelectorAll<HTMLElement>(".assistant-message")].map(
        (row) => [
          row.querySelector(".assistant-message__head")?.textContent,
          ...[...row.querySelectorAll(".assistant-tool__command")].map(
            (command) => command.textContent,
          ),
        ],
      );

    it("shows what was sent to a subagent with its calls under it, and what it reported", () => {
      draw();
      expect(rows()).toEqual([
        ["Sent toOS info", "cat /etc/os-release"],
        ["Sent toHost info", "cat /etc/hostname"],
        ["OS inforeported"],
        // The calls after a second message stand under that message.
        ["Sent toOS info", "uptime"],
        ["OS inforeported"],
      ]);
      // A report is the subagent's words, as Markdown.
      expect(screen.getByText("Debian").tagName).toBe("STRONG");
      // What it said while it worked stays in the Agents view.
      expect(screen.queryByText("Reading it now.")).toBeNull();
    });

    it("keeps the prompt closed until it is asked for", async () => {
      draw(sent.slice(0, 3));
      const prompt = screen.getByText("Read the OS release", {
        exact: false,
      });
      expect(prompt.closest("details")).not.toHaveAttribute("open");
      await userEvent.click(screen.getByText("Prompt"));
      expect(prompt.closest("details")).toHaveAttribute("open");
    });

    it("opens a subagent from its name", async () => {
      const onAgent = draw();
      const [sentTo] = screen.getAllByRole("button", { name: "OS info" });
      await userEvent.click(sentTo);
      expect(onAgent).toHaveBeenCalledExactlyOnceWith("a");
      await userEvent.click(screen.getByRole("button", { name: "Host info" }));
      expect(onAgent).toHaveBeenLastCalledWith("b");
    });

    it("shows a subagent's own subagent one level deeper", () => {
      draw([
        ["chat", announce("a", { title: "Outer" })],
        ...pair("p1", "chat", "a", "Do it"),
        ["a", call("c1", "Bash", { command: "ls /srv" })],
        ["a", announce("b", { title: "Inner" })],
        ...pair("p2", "a", "b", "Do part of it"),
        ["b", call("c2", "Bash", { command: "ls /srv/data" })],
        // What the inner one hands the outer one is not the chat's.
        ...pair("h2", "b", "a", "Part done."),
        ...pair("h1", "a", "chat", "All done."),
      ]);
      expect(rows()).toEqual([
        ["Sent toOuter", "ls /srv", "ls /srv/data"],
        ["Sent toInner", "ls /srv/data"],
        ["Outerreported"],
      ]);
      const [outer] = document.querySelectorAll(".assistant-message");
      expect(
        outer.querySelector(".assistant-message__work .assistant-message"),
      ).not.toBeNull();
      expect(screen.queryByText("Part done.")).toBeNull();
    });

    it("leaves a subagent's call out while the permission card asks about it", () => {
      draw(
        [
          ["chat", announce("a", { title: "OS info" })],
          ...pair("p1", "chat", "a", "Go"),
          ["a", call("c1", "Bash", { command: "rm -rf /tmp/x" }, "pending")],
        ],
        "c1",
      );
      expect(rows()).toEqual([["Sent toOS info"]]);
    });

    it("shows a call with no end on record as running only while its subagent runs", () => {
      const running: Sent[] = [
        ["chat", announce("a", { title: "OS info" })],
        ...pair("p1", "chat", "a", "Go"),
        ["a", call("c1", "Bash", { command: "cat /etc/hostname" }, "pending")],
      ];
      const { unmount } = render(
        <Transcript
          blocks={chatOf(running).chat.blocks}
          subagents={chatOf(running).subagents}
        />,
      );
      expect(screen.getByRole("img", { name: "running" })).toBeInTheDocument();
      unmount();
      // A loaded chat can leave a call's end out; its subagent has ended.
      draw([...running, ["chat", stateOf("a", FINISHED)]]);
      expect(screen.queryByRole("img", { name: "running" })).toBeNull();
      expect(rows()).toEqual([["Sent toOS info", "cat /etc/hostname"]]);
    });

    it("draws a launch a permission request showed once, as its message", () => {
      const { chat, subagents } = chatOf(
        [
          ["chat", announce("a", { title: "OS info" })],
          ...pair("p1", "chat", "a", "Go"),
        ],
        [
          {
            kind: "tool",
            id: "t1",
            toolCallId: "p1",
            title: "Agent: OS info",
            toolKind: "think",
            toolName: "Agent",
            status: "completed",
            content: [],
          },
        ],
      );
      render(<Transcript blocks={chat.blocks} subagents={subagents} />);
      expect(screen.queryByText("Agent: OS info")).toBeNull();
      expect(rows()).toEqual([["Sent toOS info"]]);
    });

    it("names no one for a message whose subagent it does not know", () => {
      const { chat } = chatOf(pair("p1", "chat", "gone", "Go"));
      render(<Transcript blocks={chat.blocks} />);
      expect(rows()).toEqual([["Sent toa subagent"]]);
    });
  });
});
