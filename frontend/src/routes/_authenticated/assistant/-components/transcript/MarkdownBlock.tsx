import { Icon } from "@iconify/react";
import {
  type ComponentProps,
  type ReactElement,
  type ReactNode,
  createContext,
  isValidElement,
  use,
  useEffect,
  useRef,
  useState,
} from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

import { languageForFence } from "@/components/editor/languageNames";
import AppButton from "@/components/ui/AppButton";
import { copyToClipboard } from "@/utils/clipboard";

import type { HighlightSpan } from "./highlight";

// ponytail: 20k-char ceiling; highlight completed lines only if this bites
const MAX_HIGHLIGHT_CHARS = 20_000;

// How long the Copy button says what became of the copy.
const COPY_RESULT_MS = 2000;

type CopyResult = "copied" | "failed";

const COPY_LABEL: Record<CopyResult, string> = {
  copied: "Copied",
  failed: "Copy failed",
};

// Read out for a screen reader; the button keeps its name.
const COPY_STATUS: Record<CopyResult, string> = {
  copied: "Code copied",
  failed: "Copy failed",
};

// All three stay in the DOM so the swap cross-fades both ways.
const COPY_ICONS = [
  { icon: "mdi:content-copy", shownFor: null },
  { icon: "mdi:check", shownFor: "copied" },
  { icon: "mdi:alert-circle-outline", shownFor: "failed" },
] as const;

// Loaded on demand so the CodeMirror stack stays out of the route's initial
// chunk. Module scope because React Compiler skips components with import().
const loadHighlight = () => import("./highlight");

// Fences whose body is a command for a shell, not its output.
const SHELL_FENCES = new Set(["bash", "sh", "shell", "zsh"]);

/**
 * Set by a page that can hand a command to the terminal: shell blocks under
 * it get an "Open in terminal" button. Without it (a plugin pane, a side
 * question) code can only be copied.
 */
export const OpenInTerminalContext = createContext<
  ((command: string) => void) | null
>(null);

function CodeBlock({ children }: { children?: ReactNode }) {
  const openInTerminal = use(OpenInTerminalContext);
  const preRef = useRef<HTMLPreElement>(null);
  const [copyResult, setCopyResult] = useState<CopyResult | null>(null);
  const copyTimer = useRef<number | undefined>(undefined);
  useEffect(() => () => window.clearTimeout(copyTimer.current), []);
  const showCopyResult = (result: CopyResult) => {
    setCopyResult(result);
    window.clearTimeout(copyTimer.current);
    copyTimer.current = window.setTimeout(
      () => setCopyResult(null),
      COPY_RESULT_MS,
    );
  };
  const [highlighted, setHighlighted] = useState<{
    spans: HighlightSpan[];
    text: string;
  } | null>(null);
  // react-markdown renders a fence as <pre><code class="language-x">text</code></pre>.
  const codeProps = isValidElement(children)
    ? (children as ReactElement<{ children?: ReactNode; className?: string }>)
        .props
    : undefined;
  const text =
    typeof codeProps?.children === "string" ? codeProps.children : null;
  // The fence's trailing newline is not code; it is re-appended as plain tail.
  const body = text?.replace(/\n$/, "") ?? null;
  const fence = /language-(\S+)/.exec(codeProps?.className ?? "")?.[1] ?? "";
  const lang = languageForFence(fence);

  useEffect(() => {
    if (body === null || !lang || body.length > MAX_HIGHLIGHT_CHARS) return;
    let stale = false;
    loadHighlight()
      .then(({ highlightCode }) => (stale ? null : highlightCode(body, lang)))
      .then((spans) => {
        if (spans && !stale) setHighlighted({ spans, text: body });
      })
      .catch(() => {});
    return () => {
      stale = true;
    };
  }, [body, lang]);
  // While a streamed block grows, keep the last spans and show the new text
  // unclassed, so colours do not drop out until the next highlight lands.
  const shown =
    highlighted && body !== null && body.startsWith(highlighted.text)
      ? highlighted
      : null;
  return (
    <div className="assistant-code">
      <div className="assistant-code__bar">
        <span className="assistant-code__lang">{fence || "code"}</span>
        <div className="assistant-code__actions">
          <AppButton
            aria-label="Copy code"
            className="assistant-code__copy"
            color="inherit"
            data-state={copyResult ?? undefined}
            onClick={() => {
              const code = preRef.current?.textContent ?? "";
              copyToClipboard(code.replace(/\n$/, "")).then(
                () => showCopyResult("copied"),
                () => showCopyResult("failed"),
              );
            }}
            size="small"
            startIcon={
              <span className="assistant-code__copy-icons">
                {COPY_ICONS.map(({ icon, shownFor }) => (
                  <span data-shown={shownFor === copyResult} key={icon}>
                    <Icon icon={icon} width={14} />
                  </span>
                ))}
              </span>
            }
          >
            {copyResult ? COPY_LABEL[copyResult] : "Copy"}
          </AppButton>
          <span className="assistant-code__copy-status" role="status">
            {copyResult ? COPY_STATUS[copyResult] : null}
          </span>
          {openInTerminal && body && SHELL_FENCES.has(fence) ? (
            <AppButton
              onClick={() => openInTerminal(body)}
              size="small"
              startIcon={<Icon icon="mdi:console" width={14} />}
            >
              Open in terminal
            </AppButton>
          ) : null}
        </div>
      </div>
      <pre ref={preRef}>
        {shown && text !== null ? (
          <code className={codeProps?.className}>
            {shown.spans.map((span, i) => (
              <span className={span.className} key={i}>
                {span.text}
              </span>
            ))}
            {text.slice(shown.text.length)}
          </code>
        ) : (
          children
        )}
      </pre>
    </div>
  );
}

function ExternalLink({
  node: _node,
  ...props
}: ComponentProps<"a"> & { node?: unknown }) {
  return <a {...props} rel="noopener noreferrer" target="_blank" />;
}

// Images become links: an <img> would fetch a URL the agent chose, with no click.
const components = {
  a: ExternalLink,
  img: ({ src, alt }: ComponentProps<"img">) => (
    <ExternalLink href={typeof src === "string" ? src : undefined}>
      {alt || src}
    </ExternalLink>
  ),
  pre: CodeBlock,
};

/** Agent text as GitHub-flavoured Markdown; raw HTML is not rendered. */
export default function MarkdownBlock({ text }: { text: string }) {
  return (
    <div className="assistant-markdown">
      <ReactMarkdown components={components} remarkPlugins={[remarkGfm]}>
        {text}
      </ReactMarkdown>
    </div>
  );
}
