import { useEffect, useRef, useState } from "react";

import { copyToClipboard } from "@/utils/clipboard";

export type CopyResult = "copied" | "failed";

export const COPY_RESULT_LABEL: Record<CopyResult, string> = {
  copied: "Copied",
  failed: "Copy failed",
};

// How long a copy button says what became of the copy.
const COPY_RESULT_MS = 2000;

/**
 * Copies text and holds what became of it for a moment, so the button that
 * asked can say "Copied" only once the clipboard has the text, or "Copy
 * failed" when the write was refused. Nothing to copy is not a result.
 */
export function useCopyResult() {
  const [result, setResult] = useState<CopyResult | null>(null);
  const timer = useRef<number | undefined>(undefined);
  useEffect(() => () => window.clearTimeout(timer.current), []);

  const show = (next: CopyResult) => {
    setResult(next);
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => setResult(null), COPY_RESULT_MS);
  };
  const copy = (text: string) => {
    if (!text) return;
    copyToClipboard(text).then(
      () => show("copied"),
      () => show("failed"),
    );
  };

  return { copy, result };
}
