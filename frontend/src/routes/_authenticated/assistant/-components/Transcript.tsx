import type * as acp from "@agentclientprotocol/sdk";
import { Icon } from "@iconify/react";
import { useRef, useState } from "react";

import AppButton from "@/components/ui/AppButton";
import AppTypography from "@/components/ui/AppTypography";

import DiffView from "./DiffView";
import MarkdownBlock from "./MarkdownBlock";
import type { ToolBlock, TranscriptBlock } from "./transcript";
import { useFollowBottom } from "./useFollowBottom";

function ToolRow({ block }: { block: ToolBlock }) {
  const output = block.content
    .map((c) =>
      c.type === "content" && c.content.type === "text" ? c.content.text : "",
    )
    .filter(Boolean)
    .join("\n");
  return (
    <details className="assistant-tool">
      <summary className="assistant-tool__summary">
        <AppTypography color="text.secondary" variant="caption">
          {block.toolKind} · {block.status}
        </AppTypography>
        <span className="assistant-tool__title">{block.title}</span>
      </summary>
      {block.rawInput !== undefined ? (
        <pre>{JSON.stringify(block.rawInput, null, 2)}</pre>
      ) : null}
      {block.content.map((c, index) =>
        c.type === "diff" ? (
          <DiffView
            key={index}
            newText={c.newText}
            oldText={c.oldText}
            path={c.path}
          />
        ) : null,
      )}
      {output ? <pre>{output}</pre> : null}
      {block.rawOutput !== undefined &&
      !output &&
      !block.content.some((c) => c.type === "diff") ? (
        <pre>{JSON.stringify(block.rawOutput, null, 2)}</pre>
      ) : null}
    </details>
  );
}

const DOT_LABEL = { active: "running", failed: "failed", done: "done" };

const isActive = (block: ToolBlock) =>
  block.status === "pending" || block.status === "in_progress";

/** A run of consecutive tool calls; the rows keep their block references. */
function ToolGroupRow({ tools }: { tools: ToolBlock[] }) {
  const [open, setOpen] = useState(false);
  const failed = tools.filter((tool) => tool.status === "failed").length;
  const state =
    failed > 0 ? "failed" : tools.some(isActive) ? "active" : "done";
  return (
    <div className="assistant-tool-group">
      <AppButton
        aria-expanded={open}
        className="assistant-tool-group__header"
        color="inherit"
        onClick={() => setOpen(!open)}
      >
        <span
          aria-label={DOT_LABEL[state]}
          className={`assistant-tool-group__dot assistant-tool-group__dot--${state}`}
          role="img"
          title={DOT_LABEL[state]}
        />
        <AppTypography variant="caption">
          {tools.length} tool calls
        </AppTypography>
        {failed > 0 ? (
          <AppTypography color="error" variant="caption">
            · {failed} failed
          </AppTypography>
        ) : null}
        <Icon icon={open ? "mdi:chevron-up" : "mdi:chevron-down"} width={18} />
      </AppButton>
      {open ? (
        <div className="assistant-tool-group__rows">
          {tools.map((tool) => (
            <ToolRow key={tool.id} block={tool} />
          ))}
        </div>
      ) : null}
    </div>
  );
}

type Row =
  | { key: string; block: TranscriptBlock }
  | { key: string; tools: ToolBlock[] };

/** Runs of 2+ consecutive tool blocks become one group keyed by the first call. */
function groupRows(blocks: TranscriptBlock[]): Row[] {
  const rows: Row[] = [];
  let run: ToolBlock[] = [];
  const flush = () => {
    if (run.length === 1) rows.push({ key: run[0].id, block: run[0] });
    else if (run.length > 1) {
      rows.push({ key: `group:${run[0].toolCallId}`, tools: run });
    }
    run = [];
  };
  for (const block of blocks) {
    if (block.kind === "tool") {
      run.push(block);
    } else {
      flush();
      rows.push({ key: block.id, block });
    }
  }
  flush();
  return rows;
}

function UserBlock({ text }: { text: string }) {
  return <div className="assistant-block assistant-block--user">{text}</div>;
}

function AgentBlock({ text }: { text: string }) {
  return (
    <div className="assistant-block">
      <MarkdownBlock text={text} />
    </div>
  );
}

function ThoughtBlock({ text }: { text: string }) {
  return (
    <details className="assistant-block assistant-block--thought">
      <summary>Thinking</summary>
      {text}
    </details>
  );
}

function PlanBlock({ entries }: { entries: acp.PlanEntry[] }) {
  return (
    <details className="assistant-block assistant-block--thought">
      <summary>Plan</summary>
      <ul>
        {entries.map((entry, index) => (
          <li key={index}>
            [{entry.status}] {entry.content}
          </li>
        ))}
      </ul>
    </details>
  );
}

/**
 * One component per block so React only re-renders the block that changed:
 * the reducer keeps untouched blocks by reference, and the compiler memoises
 * each row on its props. Rendering the whole conversation per token is what
 * made streaming stutter.
 */
function TranscriptRow({ block }: { block: TranscriptBlock }) {
  switch (block.kind) {
    case "user":
      return <UserBlock text={block.text} />;
    case "agent":
      return <AgentBlock text={block.text} />;
    case "thought":
      return <ThoughtBlock text={block.text} />;
    case "tool":
      return <ToolRow block={block} />;
    case "plan":
      return <PlanBlock entries={block.entries} />;
    default:
      return null;
  }
}

export default function Transcript({
  blocks,
  sessionId,
  label = "Conversation",
}: {
  blocks: TranscriptBlock[];
  sessionId?: string | null;
  label?: string;
}) {
  const ref = useRef<HTMLDivElement>(null);
  useFollowBottom(ref, sessionId);
  return (
    <div
      className="assistant__transcript"
      role="log"
      aria-label={label}
      ref={ref}
    >
      {groupRows(blocks).map((row) =>
        "tools" in row ? (
          <ToolGroupRow key={row.key} tools={row.tools} />
        ) : (
          <TranscriptRow key={row.key} block={row.block} />
        ),
      )}
    </div>
  );
}
