import { Icon } from "@iconify/react";

import { COPY_RESULT_LABEL, type CopyResult } from "./useCopyResult";

import "./app-copy-result.css";

// All three stay in the DOM so the swap cross-fades both ways.
const ICONS = [
  { icon: "mdi:content-copy", shownFor: null },
  { icon: "mdi:check", shownFor: "copied" },
  { icon: "mdi:alert-circle-outline", shownFor: "failed" },
] as const;

/** A copy button's icon: it cross-fades to a tick or a warning with the result. */
export function AppCopyResultIcon({
  result,
  size,
}: {
  result: CopyResult | null;
  size: number;
}) {
  return (
    <span className="app-copy-result__icons">
      {ICONS.map(({ icon, shownFor }) => (
        <span data-shown={shownFor === result} key={icon}>
          <Icon height={size} icon={icon} width={size} />
        </span>
      ))}
    </span>
  );
}

/**
 * Heard, not seen: a screen reader reads the result from here, so the button
 * beside it keeps its name. It goes next to the button, not inside it.
 */
export function AppCopyResultStatus({ result }: { result: CopyResult | null }) {
  return (
    <span className="app-copy-result__status" role="status">
      {result ? COPY_RESULT_LABEL[result] : null}
    </span>
  );
}
