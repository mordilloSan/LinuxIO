import AppIconButton from "@/components/ui/AppIconButton";
import AppTooltip from "@/components/ui/AppTooltip";

import type { AssistantState } from "../assistant-store";

const RADIUS = 6;
const CIRCUMFERENCE = 2 * Math.PI * RADIUS;

export const fmtTokens = (n: number) =>
  n >= 1000 ? `${(n / 1000).toFixed(1).replace(/\.0$/, "")}k` : String(n);

function fmtCost({ amount, currency }: { amount: number; currency: string }) {
  try {
    return new Intl.NumberFormat(undefined, {
      style: "currency",
      currency,
    }).format(amount);
  } catch {
    // Not an ISO 4217 code.
    return `${currency} ${amount.toFixed(2)}`;
  }
}

const level = (fraction: number) =>
  fraction > 0.9 ? "error" : fraction >= 0.7 ? "warning" : "primary";

function describe(
  usage: NonNullable<AssistantState["usage"]>,
  fraction: number,
) {
  const summary = `${fmtTokens(usage.used)} / ${fmtTokens(usage.size)} tokens (${Math.round(fraction * 100)}%)`;
  return usage.cost ? `${summary} · ${fmtCost(usage.cost)}` : summary;
}

/**
 * A small ring showing how much of the context window the chat has used.
 * Clicking it compacts the conversation when `onCompact` is given.
 */
export default function ContextGauge({
  usage,
  onCompact,
}: {
  usage: AssistantState["usage"];
  /** Absent while compacting is not possible; the ring then only reports. */
  onCompact?: () => void;
}) {
  const fraction =
    usage && usage.size > 0
      ? Math.min(Math.max(usage.used / usage.size, 0), 1)
      : 0;
  const summary = usage
    ? describe(usage, fraction)
    : "Context usage appears after the first reply";
  const text = onCompact ? `${summary} · Click to compact` : summary;

  return (
    <AppTooltip title={text}>
      {/* Never natively disabled, so the numbers stay reachable by keyboard. */}
      <AppIconButton
        aria-disabled={!onCompact}
        aria-label={text}
        className="assistant__gauge-button"
        onClick={() => onCompact?.()}
        size="small"
      >
        <svg
          aria-hidden
          className={`assistant__gauge assistant__gauge--${level(fraction)}`}
          height={16}
          viewBox="0 0 16 16"
          width={16}
        >
          <circle
            className="assistant__gauge-track"
            cx={8}
            cy={8}
            fill="none"
            r={RADIUS}
            strokeWidth={2}
          />
          <circle
            className="assistant__gauge-fill"
            cx={8}
            cy={8}
            fill="none"
            r={RADIUS}
            strokeDasharray={`${(fraction * CIRCUMFERENCE).toFixed(2)} ${CIRCUMFERENCE.toFixed(2)}`}
            strokeLinecap="round"
            strokeWidth={2}
            transform="rotate(-90 8 8)"
          />
        </svg>
      </AppIconButton>
    </AppTooltip>
  );
}
