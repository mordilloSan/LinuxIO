import { act, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { render } from "@/test/render";

import { highlightCode, type HighlightSpan } from "./highlight";
import MarkdownBlock from "./MarkdownBlock";

vi.mock("./highlight", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./highlight")>();
  return { ...actual, highlightCode: vi.fn(actual.highlightCode) };
});

describe("MarkdownBlock", () => {
  it("renders tables, lists and strikethrough", () => {
    const { container } = render(
      <MarkdownBlock
        text={"| a | b |\n|---|---|\n| 1 | 2 |\n\n- one\n- two\n\n~~gone~~"}
      />,
    );
    expect(container.querySelector("table")).not.toBeNull();
    expect(screen.getByRole("cell", { name: "2" })).toBeInTheDocument();
    expect(screen.getAllByRole("listitem")).toHaveLength(2);
    expect(container.querySelector("del")).not.toBeNull();
  });

  it("opens links in a new tab safely", () => {
    render(<MarkdownBlock text="[docs](https://example.com)" />);
    const link = screen.getByRole("link", { name: "docs" });
    expect(link).toHaveAttribute("target", "_blank");
    expect(link).toHaveAttribute("rel", "noopener noreferrer");
  });

  it("copies fenced code from a Copy button", async () => {
    const user = userEvent.setup();
    const writeText = vi.spyOn(navigator.clipboard, "writeText");
    const { container } = render(<MarkdownBlock text={"```sh\nls -la\n```"} />);
    expect(container.querySelector("pre")).toHaveTextContent("ls -la");
    await user.click(screen.getByRole("button", { name: "Copy code" }));
    expect(writeText).toHaveBeenCalledWith("ls -la");
  });

  it("does not render raw HTML", () => {
    const { container } = render(
      <MarkdownBlock text="<img src=x onerror=alert(1)>" />,
    );
    expect(container.querySelector("img")).toBeNull();
    expect(container).toHaveTextContent("<img src=x onerror=alert(1)>");
  });

  it("renders an image as a link instead of fetching it", () => {
    const { container } = render(
      <MarkdownBlock text="![leak](https://evil.test/x)" />,
    );
    expect(container.querySelector("img")).toBeNull();
    const link = screen.getByRole("link", { name: "leak" });
    expect(link).toHaveAttribute("href", "https://evil.test/x");
    expect(link).toHaveAttribute("target", "_blank");
  });

  it("does not throw when the Clipboard API is unavailable", async () => {
    const user = userEvent.setup();
    const original = Object.getOwnPropertyDescriptor(navigator, "clipboard");
    Object.defineProperty(navigator, "clipboard", {
      value: undefined,
      configurable: true,
    });
    try {
      render(<MarkdownBlock text={"```\nx\n```"} />);
      await user.click(screen.getByRole("button", { name: "Copy code" }));
      expect(screen.getByRole("button", { name: "Copy code" })).toBeEnabled();
    } finally {
      if (original) Object.defineProperty(navigator, "clipboard", original);
      else delete (navigator as { clipboard?: unknown }).clipboard;
    }
  });

  it("highlights a fenced block of a known language", async () => {
    const { container } = render(
      <MarkdownBlock text={"```js\nconst x = 1\n```"} />,
    );
    await waitFor(() =>
      expect(container.querySelector("pre .tok-keyword")).toHaveTextContent(
        "const",
      ),
    );
    expect(container.querySelector("pre")).toHaveTextContent("const x = 1");
  });

  it("leaves an unknown fence as plain text", async () => {
    vi.mocked(highlightCode).mockClear();
    const { container } = render(
      <MarkdownBlock text={"```nope\nconst x = 1\n```"} />,
    );
    expect(highlightCode).not.toHaveBeenCalled();
    expect(container.querySelector("pre span")).toBeNull();
  });

  it("skips highlighting past the size ceiling", async () => {
    vi.mocked(highlightCode).mockClear();
    const { container } = render(
      <MarkdownBlock text={"```js\n" + "x".repeat(20_001) + "\n```"} />,
    );
    await act(async () => {
      await import("./highlight");
    });
    expect(highlightCode).not.toHaveBeenCalled();
    expect(container.querySelector("pre span")).toBeNull();
  });

  it("renders a diff fence with inserted and deleted classes", async () => {
    const { container } = render(
      <MarkdownBlock text={"```diff\n+added\n-removed\n```"} />,
    );
    await waitFor(() =>
      expect(container.querySelector("pre .tok-inserted")).toHaveTextContent(
        "+added",
      ),
    );
    expect(container.querySelector("pre .tok-deleted")).toHaveTextContent(
      "-removed",
    );
  });

  it("ignores a result from an earlier run of the same text", async () => {
    const resolvers: ((spans: HighlightSpan[]) => void)[] = [];
    vi.mocked(highlightCode).mockImplementation(
      () => new Promise((resolve) => resolvers.push(resolve)),
    );
    const block = (code: string) => (
      <MarkdownBlock text={"```js\n" + code + "\n```"} />
    );
    try {
      const { container, rerender } = render(block("a"));
      await waitFor(() => expect(resolvers).toHaveLength(1));
      rerender(block("b"));
      await waitFor(() => expect(resolvers).toHaveLength(2));
      rerender(block("a"));
      await waitFor(() => expect(resolvers).toHaveLength(3));
      // Run 1 is cancelled; applying it would show its spans although the
      // text matches again, so this isolates the cleanup flag.
      await act(async () =>
        resolvers[0]([{ text: "a", className: "tok-stale" }]),
      );
      expect(container.querySelector(".tok-stale")).toBeNull();
      await act(async () =>
        resolvers[1]([{ text: "b", className: "tok-stale" }]),
      );
      expect(container.querySelector(".tok-stale")).toBeNull();
      await act(async () =>
        resolvers[2]([{ text: "a", className: "tok-fresh" }]),
      );
      expect(container.querySelector(".tok-fresh")).toHaveTextContent("a");
    } finally {
      vi.mocked(highlightCode).mockReset();
      const actual =
        await vi.importActual<typeof import("./highlight")>("./highlight");
      vi.mocked(highlightCode).mockImplementation(actual.highlightCode);
    }
  });

  it("commits nothing when a result lands after unmount", async () => {
    const error = vi.spyOn(console, "error").mockImplementation(() => {});
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
    let resolve!: (spans: HighlightSpan[]) => void;
    vi.mocked(highlightCode).mockImplementationOnce(
      () => new Promise((r) => (resolve = r)),
    );
    const { container, unmount } = render(
      <MarkdownBlock text={"```js\nx\n```"} />,
    );
    await waitFor(() => expect(resolve).toBeTypeOf("function"));
    unmount();
    resolve([{ text: "x", className: "tok-keyword" }]);
    await act(async () => {});
    expect(container).toBeEmptyDOMElement();
    expect(error).not.toHaveBeenCalled();
    expect(warn).not.toHaveBeenCalled();
    error.mockRestore();
    warn.mockRestore();
  });

  it("keeps earlier colours while a growing block re-highlights", async () => {
    const { container, rerender } = render(
      <MarkdownBlock text={"```js\nconst\n```"} />,
    );
    await waitFor(() =>
      expect(container.querySelector("pre .tok-keyword")).not.toBeNull(),
    );
    vi.mocked(highlightCode).mockImplementationOnce(
      () => new Promise(() => {}),
    );
    rerender(<MarkdownBlock text={"```js\nconst x\n```"} />);
    expect(container.querySelector("pre .tok-keyword")).toHaveTextContent(
      "const",
    );
    expect(container.querySelector("pre")).toHaveTextContent("const x");
  });
});
