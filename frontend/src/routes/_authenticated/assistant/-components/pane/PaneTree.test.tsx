import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { paneRenderAnswer } from "@/test/assistantPaneFixture";
import { render } from "@/test/render";

import PaneTree from "./PaneTree";
import { PANE_TREE_LIMITS, readPaneTree, safeHref } from "./readPaneTree";

const CONTROLS_NOTE = /controls LinuxIO cannot use yet/;
const TOO_LARGE_NOTE = /too large to show/;

const box = (children: unknown[], props: object = {}) => ({
  type: "Box",
  props,
  children,
});
const text = (children: unknown[], props: object = {}) => ({
  type: "Text",
  props,
  children,
});

describe("PaneTree", () => {
  it("draws the real lot-map render: the Svg as an <img> and the Text lines", () => {
    const { container } = render(<PaneTree tree={paneRenderAnswer.tree} />);
    const { source } = paneRenderAnswer.tree.children[0].props as {
      source: string;
    };
    const img = screen.getByRole("img", {
      name: "Work map. Nothing is running. · context 3%",
    });
    expect(img.getAttribute("src")).toBe(
      `data:image/svg+xml;charset=utf-8,${encodeURIComponent(source)}`,
    );
    expect(img).toHaveClass("assistant-pane-svg--fill");
    expect(container.querySelector("svg")).toBeNull();
    expect(container.querySelector("style")).toBeNull();
    expect(
      container.querySelector(".assistant-pane-box--column"),
    ).not.toBeNull();
    // The spaces that line the columns up are kept in the DOM.
    const line = screen.getByText("· Model    fable 5.2 · step 1  3.2 s", {
      normalizer: (value) => value,
    });
    expect(line).toHaveClass(
      "assistant-pane-text",
      "assistant-pane-text--mono",
      "assistant-pane-text--small",
      "assistant-pane-text--dim",
    );
    expect(screen.queryByText(CONTROLS_NOTE)).toBeNull();
    expect(screen.queryByText(TOO_LARGE_NOTE)).toBeNull();
  });

  it("keeps an Svg's own size and never puts its markup in the page", () => {
    const source =
      '<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>';
    const { container } = render(
      <PaneTree
        tree={{
          type: "Svg",
          props: { source, alt: "chart", width: 120, height: 9999 },
        }}
      />,
    );
    const img = screen.getByRole("img", { name: "chart" });
    expect(img).toHaveAttribute("width", "120");
    expect(img).not.toHaveAttribute("height");
    expect(img).not.toHaveClass("assistant-pane-svg--fill");
    expect(container.querySelector("svg, script")).toBeNull();
  });

  it("renders strings as text, never as HTML", () => {
    const { container } = render(
      <PaneTree tree={text(["<b>bold</b><img src=x onerror=alert(1)>"])} />,
    );
    expect(
      screen.getByText("<b>bold</b><img src=x onerror=alert(1)>"),
    ).toBeInTheDocument();
    expect(container.querySelector("b, img")).toBeNull();
  });

  it("opens http and https links in a new tab", () => {
    render(
      <PaneTree
        tree={box([
          {
            type: "Link",
            props: { href: "https://example.com/a" },
            children: ["Docs"],
          },
          {
            type: "Link",
            props: { href: "http://localhost:8080", label: "Local" },
          },
        ])}
      />,
    );
    const docs = screen.getByRole("link", { name: "Docs" });
    expect(docs).toHaveAttribute("href", "https://example.com/a");
    expect(docs).toHaveAttribute("target", "_blank");
    expect(docs).toHaveAttribute("rel", "noopener noreferrer");
    expect(screen.getByRole("link", { name: "Local" })).toHaveAttribute(
      "href",
      "http://localhost:8080/",
    );
  });

  it.each([
    "javascript:alert(1)",
    "JaVaScRiPt:alert(1)",
    "data:text/html,<script>alert(1)</script>",
    "file:///etc/passwd",
    "https://user:secret@example.com",
    "/relative",
  ])("does not make %s a link", (href) => {
    const { container } = render(
      <PaneTree tree={{ type: "Link", props: { href }, children: ["Open"] }} />,
    );
    expect(container.querySelector("a")).toBeNull();
    expect(screen.getByText("Open")).toBeInTheDocument();
    expect(safeHref(href)).toBeNull();
  });

  it("draws Markdown and Code", () => {
    const { container } = render(
      <PaneTree
        tree={box([
          {
            type: "Markdown",
            props: { text: "**Plan** [x](javascript:alert(1))" },
          },
          { type: "Code", props: { source: "<script>alert(1)</script>" } },
        ])}
      />,
    );
    expect(screen.getByText("Plan").tagName).toBe("STRONG");
    expect(container.querySelector('a[href^="javascript"]')).toBeNull();
    expect(container.querySelector("pre code")?.textContent).toBe(
      "<script>alert(1)</script>",
    );
    expect(container.querySelector("script")).toBeNull();
  });

  it("leaves out controls and says so once", () => {
    render(
      <PaneTree
        tree={box([
          text(["Before"]),
          {
            type: "Button",
            props: { key: "go", label: "Run" },
            children: ["Run"],
          },
          { type: "Input", props: { key: "q", placeholder: "Ask" } },
          { type: "Select", props: { key: "s", options: [] } },
          text(["After"]),
        ])}
      />,
    );
    expect(screen.getByText("Before")).toBeInTheDocument();
    expect(screen.getByText("After")).toBeInTheDocument();
    expect(screen.queryByRole("button")).toBeNull();
    expect(screen.queryByRole("textbox")).toBeNull();
    expect(screen.queryByText("Run")).toBeNull();
    expect(screen.getAllByText(CONTROLS_NOTE)).toHaveLength(1);
  });

  it("keeps the children of an unknown node and mentions it", () => {
    render(
      <PaneTree tree={{ type: "Spinner", children: [text(["Inside"])] }} />,
    );
    expect(screen.getByText("Inside")).toBeInTheDocument();
    expect(screen.getByText(CONTROLS_NOTE)).toBeInTheDocument();
  });

  it("maps semantic colours and ignores what it cannot map", () => {
    render(
      <PaneTree
        tree={box(
          [
            text(["Failed"], { color: "error" }),
            text(["Red"], { color: "ansi:redBright", bold: true }),
            text(["Hex"], { color: "#ff0000", size: "enormous" }),
          ],
          {
            gap: 1,
            padding: 5,
            alignItems: "center",
            flexDirection: "sideways",
          },
        )}
      />,
    );
    expect(screen.getByText("Failed")).toHaveClass(
      "assistant-pane-text--error",
    );
    expect(screen.getByText("Red")).toHaveClass(
      "assistant-pane-text--error",
      "assistant-pane-text--bold",
    );
    expect(screen.getByText("Hex").className).toBe("assistant-pane-text");
    expect(screen.getByText("Hex").parentElement?.className).toBe(
      "assistant-pane-box assistant-pane-box--align-center assistant-pane-box--gap-1 assistant-pane-box--pad-3",
    );
  });

  it("draws nothing for a Box with display none", () => {
    render(<PaneTree tree={box([text(["Hidden"])], { display: "none" })} />);
    expect(screen.queryByText("Hidden")).toBeNull();
  });
});

describe("readPaneTree limits", () => {
  it("stops at the depth limit", () => {
    let tree: unknown = text(["deepest"]);
    for (let i = 0; i < 10_000; i++) tree = box([tree]);
    const read = readPaneTree(tree);
    expect(read.truncated).toBe(true);
    let depth = 0;
    let item = read.items[0];
    while (item && typeof item !== "string" && "children" in item) {
      depth++;
      item = item.children[0];
    }
    expect(depth).toBeLessThanOrEqual(PANE_TREE_LIMITS.depth);
  });

  it("stops at the node limit without walking the rest", () => {
    const children: unknown[] = Array.from({ length: 200_000 }, (_, i) =>
      text([`${i}`]),
    );
    Object.defineProperty(children, PANE_TREE_LIMITS.nodes, {
      get() {
        throw new Error("read past the node limit");
      },
    });
    const read = readPaneTree(box(children));
    expect(read.truncated).toBe(true);
    const root = read.items[0];
    if (typeof root === "string" || !("children" in root)) {
      throw new Error("expected a box");
    }
    expect(root.children.length).toBeLessThan(PANE_TREE_LIMITS.nodes);
  });

  it("stops at the character limit", () => {
    const big = "x".repeat(PANE_TREE_LIMITS.chars);
    const read = readPaneTree(
      box([text(["a"]), { type: "Code", props: { source: big } }]),
    );
    expect(read.truncated).toBe(true);
    expect(JSON.stringify(read.items)).not.toContain(big);
  });

  it("says when part of the pane was left out", () => {
    let tree: unknown = text(["deepest"]);
    for (let i = 0; i < 100; i++) tree = box([tree]);
    render(<PaneTree tree={tree} />);
    expect(screen.getByText(TOO_LARGE_NOTE)).toBeInTheDocument();
  });

  it("draws nothing for a tree that is not a node", () => {
    for (const tree of [null, 3, "text", [], { props: {} }]) {
      const read = readPaneTree(tree);
      expect(read.unsupported).toBe(false);
      if (typeof tree === "string" || typeof tree === "number") {
        expect(read.items).toEqual([String(tree)]);
      } else {
        expect(read.items).toEqual([]);
      }
    }
  });
});
