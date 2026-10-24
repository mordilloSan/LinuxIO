import { Icon } from "@iconify/react";
import { useLayoutEffect, type RefObject } from "react";

import "./install-output-panel.css";

import AppButton from "@/components/ui/AppButton";
import AppTypography from "@/components/ui/AppTypography";

export const MAX_INSTALL_OUTPUT_LINES = 500;

export type InstallOutputStream = "status" | "stderr" | "stdout";

export interface InstallOutputLine {
  id: number;
  stream: InstallOutputStream;
  text: string;
}

interface InstallOutputPanelProps {
  className?: string;
  expanded: boolean;
  id: string;
  /** Completes "Show …" / "Hide …" on the toggle. */
  label: string;
  onToggle: () => void;
  output: InstallOutputLine[];
  outputHistoryIncomplete?: boolean;
  /** Owned by the host so it can re-pin the scroll after its own transitions. */
  scrollerRef: RefObject<HTMLDivElement | null>;
}

/**
 * Collapsible installer/package-manager output. The host sizes it: the panel
 * is the toggle height when collapsed and grows to whatever min-height the
 * host gives `.install-output--expanded`.
 */
const InstallOutputPanel = ({
  className,
  expanded,
  id,
  label,
  onToggle,
  output,
  outputHistoryIncomplete = false,
  scrollerRef,
}: InstallOutputPanelProps) => {
  useLayoutEffect(() => {
    const scroller = scrollerRef.current;
    if (expanded && scroller) {
      scroller.scrollTop = scroller.scrollHeight;
    }
    // New output intentionally retriggers scrolling even though only the DOM
    // height, not the output value itself, is read inside the Effect.
    // oxlint-disable-next-line react/exhaustive-effect-dependencies
  }, [output, expanded, scrollerRef]);

  return (
    <div
      className={`install-output ${expanded ? "install-output--expanded" : ""} ${
        className ?? ""
      }`.trim()}
    >
      <AppButton
        aria-controls={id}
        aria-expanded={expanded}
        className="install-output__toggle"
        onClick={onToggle}
        type="button"
      >
        <Icon
          className={`install-output__chevron ${
            expanded ? "install-output__chevron--expanded" : ""
          }`.trim()}
          height={18}
          icon="mdi:chevron-right"
          width={18}
        />
        <AppTypography color="text.secondary" variant="caption">
          {expanded ? `Hide ${label}` : `Show ${label}`}
        </AppTypography>
      </AppButton>
      <div className="install-output__animator">
        <div
          aria-hidden={!expanded}
          className="install-output__scroller"
          id={id}
          ref={scrollerRef}
          style={{
            backgroundColor: "var(--app-code-block-background)",
            color: "var(--app-code-block-color)",
          }}
        >
          <div className="install-output__lines">
            {outputHistoryIncomplete ? (
              <div className="install-output__note">
                [status] Earlier output may not have been retained.
              </div>
            ) : null}
            {output.map((line) => (
              <div
                className={`install-output__line install-output__line--${line.stream}`}
                key={line.id}
              >
                {line.stream === "status" ? `[status] ${line.text}` : line.text}
              </div>
            ))}
            {output.length === 0 && !outputHistoryIncomplete ? (
              <div className="install-output__note">
                Waiting for installer output…
              </div>
            ) : null}
          </div>
        </div>
      </div>
    </div>
  );
};

export default InstallOutputPanel;
