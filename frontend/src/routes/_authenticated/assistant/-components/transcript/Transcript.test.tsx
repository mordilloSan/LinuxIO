import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

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
    await userEvent.click(screen.getByRole("button", { name: /Edit file/ }));
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
    expect(screen.getByText("Run")).toBeInTheDocument();
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

  it("copies an answer", async () => {
    const user = userEvent.setup();
    render(
      <Transcript blocks={[{ kind: "agent", id: "1", text: "**hi**" }]} />,
    );
    await user.click(screen.getByRole("button", { name: "Copy answer" }));
    expect(await navigator.clipboard.readText()).toBe("**hi**");
  });

  describe("tool runs", () => {
    const read = (id: string, status = "completed") => check(id, status);

    it("lists checks as log lines, with no card", () => {
      render(<Transcript blocks={[read("a"), read("b"), read("c")]} />);
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

    it("says the card is running while a step is in progress", () => {
      render(<Transcript blocks={[tool("a"), tool("b", "in_progress")]} />);
      expect(
        screen.getByRole("group", { name: "Running on this server" }),
      ).toBeInTheDocument();
    });

    it("counts failed steps on the card and marks the line", () => {
      render(<Transcript blocks={[tool("a"), tool("b", "failed")]} />);
      expect(screen.getByText("1 failed")).toBeInTheDocument();
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
});
