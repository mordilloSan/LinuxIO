import { render, screen } from "@testing-library/react";
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

  it("labels the log with the label prop", () => {
    render(<Transcript blocks={[]} label="Side question" />);
    expect(
      screen.getByRole("log", { name: "Side question" }),
    ).toBeInTheDocument();
  });

  it("renders a diff view for edit tool calls", () => {
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
    expect(container.querySelector(".assistant-diff__add")).toHaveTextContent(
      "+y",
    );
    expect(container.querySelector(".assistant-diff__del")).toHaveTextContent(
      "-x",
    );
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
      await userEvent.click(screen.getByText("3 tool calls"));
      expect(screen.queryByText("cmd a")).toBeNull();
    });

    it("shows a failed count and a failed dot when any tool failed", () => {
      const { container } = render(
        <Transcript blocks={[tool("a"), tool("b", "failed")]} />,
      );
      expect(screen.getByText("· 1 failed")).toBeInTheDocument();
      expect(
        container.querySelector(".assistant-tool-group__dot--failed"),
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
        container.querySelector(".assistant-tool-group__dot--active"),
      ).not.toBeNull();
    });

    it("marks a finished group done", () => {
      const { container } = render(
        <Transcript blocks={[tool("a"), tool("b")]} />,
      );
      expect(
        container.querySelector(".assistant-tool-group__dot--done"),
      ).not.toBeNull();
    });

    it("keeps a single tool as a plain row", () => {
      render(<Transcript blocks={[tool("a")]} />);
      expect(screen.getByText("cmd a")).toBeInTheDocument();
      expect(screen.queryByText(/tool calls?$/)).toBeNull();
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
