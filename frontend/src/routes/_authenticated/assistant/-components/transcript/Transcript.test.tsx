import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import Transcript from "./Transcript";
import type { TranscriptBlock } from "./transcript";

const tool = (id: string, status = "completed"): TranscriptBlock => ({
  kind: "tool",
  id: `b-${id}`,
  toolCallId: id,
  title: `cmd ${id}`,
  toolKind: "execute",
  status,
  content: [],
});

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

  it("renders a diff view for edit tool calls once opened", async () => {
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
    await userEvent.click(screen.getByRole("button", { name: /1 tool call/ }));
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
    // A lone call is a closed group of one, like any other run.
    expect(screen.queryByText("IN")).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: /1 tool call/ }));
    expect(screen.getByText("Run")).toBeInTheDocument();
    expect(screen.getByText("List files")).toBeInTheDocument();
    expect(screen.getByText("IN")).toBeInTheDocument();
    expect(screen.queryByText(/```/)).toBeNull();
    expect(screen.getByText("ls -la")).toHaveClass("assistant-tool__in");
    const out = screen.getByText(/line 0/);
    expect(out).toHaveClass("assistant-tool__out--clipped");
    await userEvent.click(
      screen.getByRole("button", { name: "Show all 12 lines" }),
    );
    expect(out).not.toHaveClass("assistant-tool__out--clipped");
  });

  describe("grouped tool calls", () => {
    it("collapses 3 consecutive tools into one closed row that opens on click", async () => {
      render(<Transcript blocks={[tool("a"), tool("b"), tool("c")]} />);
      expect(screen.getAllByText(/tool calls?$/)).toHaveLength(1);
      expect(screen.queryByText("cmd a")).toBeNull();
      await userEvent.click(screen.getByText("3 tool calls"));
      for (const id of ["a", "b", "c"]) {
        expect(screen.getByText(`cmd ${id}`)).toBeInTheDocument();
      }
      await userEvent.click(screen.getByRole("button", { name: "Collapse" }));
      expect(screen.queryByText("cmd a")).toBeNull();
      expect(
        screen.getByRole("button", { name: /3 tool calls/ }),
      ).toHaveFocus();
    });

    it("shows a failed count and a failed dot when any tool failed", () => {
      const { container } = render(
        <Transcript blocks={[tool("a"), tool("b", "failed")]} />,
      );
      expect(screen.getByText("· 1 failed")).toBeInTheDocument();
      expect(
        container.querySelector(".assistant-tool-state--failed"),
      ).not.toBeNull();
      expect(screen.getByRole("img", { name: "failed" })).toHaveAttribute(
        "title",
        "failed",
      );
    });

    it("marks the group active while a tool is pending or in progress", () => {
      const { container } = render(
        <Transcript blocks={[tool("a"), tool("b", "in_progress")]} />,
      );
      expect(
        container.querySelector(".assistant-tool-state--active"),
      ).not.toBeNull();
    });

    it("marks a finished group done", () => {
      const { container } = render(
        <Transcript blocks={[tool("a"), tool("b")]} />,
      );
      expect(
        container.querySelector(".assistant-tool-state--done"),
      ).not.toBeNull();
    });

    it("collapses a single tool into a group of one", async () => {
      render(<Transcript blocks={[tool("a")]} />);
      expect(screen.queryByText("cmd a")).toBeNull();
      await userEvent.click(screen.getByText("1 tool call"));
      expect(screen.getByText("cmd a")).toBeInTheDocument();
    });

    it("makes two groups when an agent block separates the tools", () => {
      render(
        <Transcript
          blocks={[
            tool("a"),
            tool("b"),
            { kind: "agent", id: "x", text: "between" },
            tool("c"),
            tool("d"),
          ]}
        />,
      );
      expect(screen.getAllByText("2 tool calls")).toHaveLength(2);
      expect(screen.getByText("between")).toBeInTheDocument();
    });

    it("keeps the open state when a tool joins the group", async () => {
      const { rerender } = render(
        <Transcript blocks={[tool("a"), tool("b")]} />,
      );
      await userEvent.click(screen.getByText("2 tool calls"));
      rerender(<Transcript blocks={[tool("a"), tool("b"), tool("c")]} />);
      expect(screen.getByText("3 tool calls")).toBeInTheDocument();
      expect(screen.getByText("cmd c")).toBeInTheDocument();
    });
  });
});
