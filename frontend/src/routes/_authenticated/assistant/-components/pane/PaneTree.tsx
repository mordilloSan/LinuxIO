import type { ReactNode } from "react";

import AppTypography from "@/components/ui/AppTypography";

import MarkdownBlock from "../transcript/MarkdownBlock";
import { type PaneItem, readPaneTree } from "./readPaneTree";

function renderItems(items: PaneItem[]): ReactNode[] {
  return items.map((item, index) => {
    if (typeof item === "string") return item;
    switch (item.kind) {
      case "box":
        return (
          <div className={item.className} key={index}>
            {renderItems(item.children)}
          </div>
        );
      case "text":
        return (
          <span className={item.className} key={index}>
            {renderItems(item.children)}
          </span>
        );
      case "svg":
        // An <img> runs no script and loads nothing; the SVG never joins the DOM.
        return (
          <img
            alt={item.alt}
            className={
              item.width === undefined && item.height === undefined
                ? "assistant-pane-svg assistant-pane-svg--fill"
                : "assistant-pane-svg"
            }
            height={item.height}
            key={index}
            src={item.src}
            width={item.width}
          />
        );
      case "markdown":
        return (
          <div
            className={item.dim ? "assistant-pane-text--dim" : undefined}
            key={index}
          >
            <MarkdownBlock text={item.text} />
          </div>
        );
      case "code":
        return (
          <pre className="assistant-pane-code" key={index}>
            <code>{item.source}</code>
          </pre>
        );
      case "link":
        return item.href ? (
          <a
            className="assistant-pane-link"
            href={item.href}
            key={index}
            rel="noopener noreferrer"
            target="_blank"
            title={item.href}
          >
            {renderItems(item.children)}
          </a>
        ) : (
          <span className="assistant-pane-text" key={index}>
            {renderItems(item.children)}
          </span>
        );
    }
  });
}

/** Draws a plugin's render tree read-only; see readPaneTree for what is kept. */
export default function PaneTree({ tree }: { tree: unknown }) {
  const { items, unsupported, truncated } = readPaneTree(tree);
  return (
    <>
      <div className="assistant-pane">{renderItems(items)}</div>
      {truncated ? (
        <AppTypography color="text.secondary" variant="body2">
          Part of this pane is too large to show.
        </AppTypography>
      ) : null}
      {unsupported ? (
        <AppTypography color="text.secondary" variant="body2">
          This pane has controls LinuxIO cannot use yet.
        </AppTypography>
      ) : null}
    </>
  );
}
