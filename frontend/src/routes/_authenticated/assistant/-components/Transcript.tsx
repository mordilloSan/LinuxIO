import type * as acp from "@agentclientprotocol/sdk";
import { Icon } from "@iconify/react";
import { type ReactNode, useRef, useState } from "react";

import AppButton from "@/components/ui/AppButton";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppTypography from "@/components/ui/AppTypography";

import AttachmentChip from "./AttachmentChip";
import type { Attachment } from "./attachments";
import DiffView from "./DiffView";
import MarkdownBlock from "./MarkdownBlock";
import {
  stringField,
  type ToolBlock,
  type TranscriptBlock,
} from "./transcript";
import { useFollowBottom } from "./useFollowBottom";

const KIND_LABEL: Record<string, string> = {
  read: "Read",
  edit: "Edit",
  delete: "Delete",
  move: "Move",
  search: "Search",
  execute: "Run",
  think: "Think",
  fetch: "Fetch",
  switch_mode: "Mode",
};

type ToolState = "active" | "failed" | "done";

const STATE_LABEL = { active: "running", failed: "failed", done: "done" };

const toolState = (status: string): ToolState =>
  status === "failed"
    ? "failed"
    : status === "pending" || status === "in_progress"
      ? "active"
      : "done";

/** Running spins, failed is an alert, done is a dot: the shape carries it. */
function StateMarker({ state }: { state: ToolState }) {
  return (
    <span
      aria-label={STATE_LABEL[state]}
      className={`assistant-tool-state assistant-tool-state--${state}`}
      role="img"
      title={STATE_LABEL[state]}
    >
      {state === "active" ? <AppCircularProgress size={10} /> : null}
      {state === "failed" ? <Icon icon="mdi:alert-circle" width={12} /> : null}
    </span>
  );
}

// Output longer than this starts clipped behind a fade.
const CLIP_LINES = 8;

function ToolOutput({ text }: { text: string }) {
  const [expanded, setExpanded] = useState(false);
  const lines = text.split("\n").length;
  const long = lines > CLIP_LINES;
  return (
    <div className="assistant-tool__io-row assistant-tool__io-row--out">
      <span className="assistant-tool__io-label">OUT</span>
      <div className="assistant-tool__io-cell">
        <pre
          className={
            long && !expanded
              ? "assistant-tool__out assistant-tool__out--clipped"
              : "assistant-tool__out"
          }
        >
          {text}
        </pre>
        {long ? (
          <AppButton
            className="assistant-tool__more"
            color="inherit"
            onClick={() => setExpanded(!expanded)}
            size="small"
          >
            {expanded ? "Show less" : `Show all ${lines} lines`}
          </AppButton>
        ) : null}
      </div>
    </div>
  );
}

/** One tool call: what it does, then its input and output, like the IDE. */
function ToolRow({ block }: { block: ToolBlock }) {
  const command = stringField(block.rawInput, "command");
  const hasInput =
    typeof block.rawInput === "object" &&
    block.rawInput !== null &&
    Object.keys(block.rawInput).length > 0;
  const input =
    command ?? (hasInput ? JSON.stringify(block.rawInput, null, 2) : null);
  const description = stringField(block.rawInput, "description");
  const detail = description ?? (block.title !== command ? block.title : null);
  const diffs = block.content.flatMap((c) => (c.type === "diff" ? [c] : []));
  const text = block.content
    .map((c) =>
      c.type === "content" && c.content.type === "text" ? c.content.text : "",
    )
    .filter(Boolean)
    .join("\n")
    // Agents often wrap output in a Markdown fence; the box already reads as code.
    .replace(/^```[^\n]*\n([\s\S]*?)\n?```\s*$/, "$1");
  const output =
    text ||
    (block.rawOutput !== undefined && diffs.length === 0
      ? JSON.stringify(block.rawOutput, null, 2)
      : "");
  return (
    <div className="assistant-tool">
      <StateMarker state={toolState(block.status)} />
      <div className="assistant-tool__head">
        <span className="assistant-tool__kind">
          {KIND_LABEL[block.toolKind] ?? "Tool"}
        </span>
        {detail ? (
          <span className="assistant-tool__detail">{detail}</span>
        ) : null}
      </div>
      {input || output ? (
        <div className="assistant-tool__io">
          {input ? (
            <div className="assistant-tool__io-row">
              <span className="assistant-tool__io-label">IN</span>
              <pre className="assistant-tool__in">{input}</pre>
            </div>
          ) : null}
          {output ? <ToolOutput text={output} /> : null}
        </div>
      ) : null}
      {diffs.map((diff) => (
        <DiffView
          key={diff.path}
          newText={diff.newText}
          oldText={diff.oldText}
          path={diff.path}
        />
      ))}
    </div>
  );
}

/** A run of consecutive tool calls; the rows keep their block references. */
function ToolGroupRow({ tools }: { tools: ToolBlock[] }) {
  const [open, setOpen] = useState(false);
  const headerRef = useRef<HTMLButtonElement>(null);
  const failed = tools.filter((tool) => tool.status === "failed").length;
  const state: ToolState =
    failed > 0
      ? "failed"
      : tools.some((tool) => toolState(tool.status) === "active")
        ? "active"
        : "done";
  return (
    <div className="assistant-tool-group">
      <AppButton
        aria-expanded={open}
        className="assistant-tool-group__header"
        color="inherit"
        onClick={() => setOpen(!open)}
        ref={headerRef}
      >
        <StateMarker state={state} />
        <AppTypography variant="caption">
          {tools.length} {tools.length === 1 ? "tool call" : "tool calls"}
        </AppTypography>
        {failed > 0 ? (
          <AppTypography color="error" variant="caption">
            · {failed} failed
          </AppTypography>
        ) : null}
        <Icon icon={open ? "mdi:chevron-up" : "mdi:chevron-down"} width={18} />
      </AppButton>
      {open ? (
        <>
          <div className="assistant-tool-list">
            {tools.map((tool) => (
              <ToolRow key={tool.id} block={tool} />
            ))}
          </div>
          <AppButton
            className="assistant-tool-group__collapse"
            color="inherit"
            onClick={() => {
              setOpen(false);
              headerRef.current?.focus();
            }}
            size="small"
          >
            Collapse
            <Icon icon="mdi:chevron-up" width={18} />
          </AppButton>
        </>
      ) : null}
    </div>
  );
}

type Row =
  | { key: string; block: TranscriptBlock }
  | { key: string; tools: ToolBlock[] };

/** Every run of consecutive tool blocks becomes one group keyed by the first call. */
function groupRows(blocks: TranscriptBlock[]): Row[] {
  const rows: Row[] = [];
  let run: ToolBlock[] = [];
  const flush = () => {
    if (run.length > 0) {
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

function UserBlock({
  text,
  attachments,
}: {
  text: string;
  attachments: Attachment[];
}) {
  return (
    <div className="assistant-block assistant-block--user">
      {attachments.length > 0 ? (
        <div className="assistant__attachments">
          {attachments.map((attachment) => (
            <AttachmentChip attachment={attachment} key={attachment.id} />
          ))}
        </div>
      ) : null}
      {text}
    </div>
  );
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
      return <UserBlock attachments={block.attachments} text={block.text} />;
    case "agent":
      return <AgentBlock text={block.text} />;
    case "thought":
      return <ThoughtBlock text={block.text} />;
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
  children,
}: {
  blocks: TranscriptBlock[];
  sessionId?: string | null;
  label?: string;
  /** Shown in place of the rows while the conversation is empty. */
  children?: ReactNode;
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
      {blocks.length === 0
        ? children
        : groupRows(blocks).map((row) =>
            "tools" in row ? (
              <ToolGroupRow key={row.key} tools={row.tools} />
            ) : (
              <TranscriptRow key={row.key} block={row.block} />
            ),
          )}
    </div>
  );
}
