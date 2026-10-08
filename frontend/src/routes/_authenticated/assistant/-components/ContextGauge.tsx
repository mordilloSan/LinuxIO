import AppTooltip from "@/components/ui/AppTooltip";

import type { AssistantState } from "./assistant-store";

const RADIUS = 6;
const CIRCUMFERENCE = 2 * Math.PI * RADIUS;

const fmtTokens = (n: number) =>
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

/** A small ring showing how much of the context window the chat has used. */
export default function ContextGauge({
  usage,
}: {
  usage: AssistantState["usage"];
}) {
  if (!usage) return null;
  const fraction =
    usage.size > 0 ? Math.min(Math.max(usage.used / usage.size, 0), 1) : 0;
  const summary = `${fmtTokens(usage.used)} / ${fmtTokens(usage.size)} tokens (${Math.round(fraction * 100)}%)`;
  const text = usage.cost ? `${summary} · ${fmtCost(usage.cost)}` : summary;

  return (
    <AppTooltip title={text}>
      <span className="assistant__gauge-wrap">
        <svg
          aria-label={text}
          className={`assistant__gauge assistant__gauge--${level(fraction)}`}
          height={16}
          role="img"
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
      </span>
    </AppTooltip>
  );
}
