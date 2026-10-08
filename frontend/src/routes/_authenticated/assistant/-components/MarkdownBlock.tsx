import {
  type ComponentProps,
  type ReactElement,
  type ReactNode,
  isValidElement,
  useEffect,
  useRef,
  useState,
} from "react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

import { languageForFence } from "@/components/filebrowser/languageNames";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import { copyToClipboard } from "@/utils/clipboard";

import type { HighlightSpan } from "./highlight";

// ponytail: 20k-char ceiling; highlight completed lines only if this bites
const MAX_HIGHLIGHT_CHARS = 20_000;

function CodeBlock({ children }: { children?: ReactNode }) {
  const preRef = useRef<HTMLPreElement>(null);
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
  const lang = languageForFence(
    /language-(\S+)/.exec(codeProps?.className ?? "")?.[1] ?? "",
  );

  useEffect(() => {
    if (body === null || !lang || body.length > MAX_HIGHLIGHT_CHARS) return;
    let stale = false;
    // Loaded here so the CodeMirror stack stays out of the route's initial chunk.
    import("./highlight")
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
      <AppActionIconButton
        ariaLabel="Copy code"
        className="assistant-code__copy"
        icon="mdi:content-copy"
        iconSize={16}
        label="Copy code"
        onClick={() => {
          const code = preRef.current?.textContent ?? "";
          copyToClipboard(code.replace(/\n$/, "")).catch(() => {});
        }}
        size="small"
      />
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
