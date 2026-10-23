import { Icon } from "@iconify/react";
import { useId, useState, type CSSProperties, type ReactNode } from "react";

import FrostedCard from "@/components/cards/FrostedCard";
import AppButton from "@/components/ui/AppButton";
import AppCollapse from "@/components/ui/AppCollapse";
import AppIconButton from "@/components/ui/AppIconButton";
import AppTypography from "@/components/ui/AppTypography";
import { formatThroughput } from "@/utils/formatters";

export const STATUS_COLOR = {
  error: "var(--app-palette-error-main)",
  muted: "var(--app-palette-text-disabled)",
  ok: "var(--app-palette-success-main)",
  warning: "var(--app-palette-warning-main)",
} as const;

type StatusTone = keyof typeof STATUS_COLOR;

const STATUS_ICON: Record<StatusTone, string> = {
  error: "mdi:close-circle",
  muted: "mdi:minus-circle",
  ok: "mdi:check-circle",
  warning: "mdi:alert-circle",
};

const headerStyle: CSSProperties = {
  alignItems: "center",
  display: "grid",
  gap: "var(--app-space-16)",
  gridTemplateColumns: "auto minmax(140px, 1fr) minmax(0, 1.2fr) auto",
  padding: "var(--app-space-12) var(--app-space-16)",
};

const bodyStyle: CSSProperties = {
  borderTop: "1px solid var(--app-palette-divider)",
  display: "flex",
  flexDirection: "column",
  gap: "var(--app-space-12)",
  padding: "var(--app-space-12) var(--app-space-16) var(--app-space-16)",
};

const keyValueStyle: CSSProperties = {
  columnGap: "var(--app-space-16)",
  display: "grid",
  gridTemplateColumns: "minmax(140px, 1fr) minmax(0, 2fr)",
  rowGap: "var(--app-space-4)",
};

/** A status line with a coloured icon, as under every TOS-style card title. */
export function StatusLine({ text, tone }: { text: string; tone: StatusTone }) {
  return (
    <AppTypography
      noWrap
      style={{
        alignItems: "center",
        color: STATUS_COLOR[tone],
        display: "inline-flex",
        gap: 4,
      }}
      variant="caption"
    >
      <Icon icon={STATUS_ICON[tone]} width={14} />
      {text}
    </AppTypography>
  );
}

/** A collapsible card: icon, name and status on the left, a summary on the right. */
export function EntityCard({
  children,
  icon,
  name,
  status,
  summary,
  tone,
}: {
  children: ReactNode;
  icon: string;
  name: string;
  status: string;
  summary?: ReactNode;
  tone: StatusTone;
}) {
  const [expanded, setExpanded] = useState(true);
  const panelId = useId();
  return (
    <FrostedCard aria-label={name} role="region">
      <div style={headerStyle}>
        <Icon
          color={
            tone === "muted"
              ? STATUS_COLOR.muted
              : "var(--app-palette-primary-main)"
          }
          height={36}
          icon={icon}
          width={36}
        />
        <div style={{ minWidth: 0 }}>
          <AppTypography component="h2" fontWeight={600} noWrap variant="body1">
            {name}
          </AppTypography>
          <StatusLine text={status} tone={tone} />
        </div>
        <div style={{ minWidth: 0 }}>{summary}</div>
        <AppIconButton
          aria-controls={panelId}
          aria-expanded={expanded}
          aria-label={expanded ? `Collapse ${name}` : `Expand ${name}`}
          onClick={() => setExpanded((open) => !open)}
          size="small"
        >
          <Icon
            icon="mdi:chevron-up"
            style={{
              transform: expanded ? "rotate(0deg)" : "rotate(180deg)",
              transition:
                "transform var(--app-transition-duration-fast) var(--app-easing-standard)",
            }}
            width={22}
          />
        </AppIconButton>
      </div>
      <div id={panelId}>
        <AppCollapse in={expanded} unmountOnExit>
          <div style={bodyStyle}>{children}</div>
        </AppCollapse>
      </div>
    </FrostedCard>
  );
}

/** Label / value pairs in two columns. */
export function KeyValues({ items }: { items: [string, ReactNode][] }) {
  return (
    <dl style={{ ...keyValueStyle, margin: 0 }}>
      {items.map(([label, value]) => (
        <div key={label} style={{ display: "contents" }}>
          <AppTypography color="text.secondary" component="dt" variant="body2">
            {label}
          </AppTypography>
          <AppTypography
            component="dd"
            fontWeight={600}
            style={{ margin: 0, overflowWrap: "anywhere" }}
            variant="body2"
          >
            {value}
          </AppTypography>
        </div>
      ))}
    </dl>
  );
}

/** Mutually exclusive pills, such as the Host / Virtual machines switch. */
export function PillToggle<T extends string>({
  ariaLabel,
  onChange,
  options,
  value,
}: {
  ariaLabel: string;
  onChange: (next: T) => void;
  options: readonly { label: string; value: T }[];
  value: T;
}) {
  return (
    <div
      aria-label={ariaLabel}
      role="group"
      style={{ display: "flex", flexWrap: "wrap", gap: "var(--app-space-4)" }}
    >
      {options.map((option) => (
        <AppButton
          aria-pressed={value === option.value}
          key={option.value}
          onClick={() => onChange(option.value)}
          size="small"
          variant={value === option.value ? "contained" : "text"}
        >
          {option.label}
        </AppButton>
      ))}
    </div>
  );
}

/** Receive and send rates with coloured arrows; a dash when no sample exists. */
export function Throughput({ rx, tx }: { rx?: number; tx?: number }) {
  const rate = (value: number | undefined) =>
    value === undefined ? "-" : formatThroughput(value);
  return (
    <span
      style={{
        display: "inline-flex",
        gap: "var(--app-space-12)",
        whiteSpace: "nowrap",
      }}
    >
      <span style={{ color: STATUS_COLOR.ok }}>
        <Icon icon="mdi:arrow-down-bold" inline /> {rate(rx)}
      </span>
      <span style={{ color: "var(--app-palette-primary-main)" }}>
        <Icon icon="mdi:arrow-up-bold" inline /> {rate(tx)}
      </span>
    </span>
  );
}

const tileStyle: CSSProperties = {
  alignItems: "center",
  display: "grid",
  gap: "var(--app-space-8)",
  gridTemplateColumns: "1fr auto",
  padding: "var(--app-space-16)",
};

/** A count tile: icon and label on top, a big number and a status under it. */
export function Tile({
  count,
  icon,
  label,
  status,
  statusColor,
}: {
  count: ReactNode;
  icon: string;
  label: string;
  status: string;
  statusColor: string;
}) {
  return (
    <FrostedCard hoverLift style={tileStyle}>
      <div
        style={{
          alignItems: "center",
          display: "flex",
          gap: "var(--app-space-8)",
          gridColumn: "1 / -1",
        }}
      >
        <Icon
          color="var(--app-palette-primary-main)"
          height={20}
          icon={icon}
          width={20}
        />
        <AppTypography fontWeight={600} variant="body2">
          {label}
        </AppTypography>
      </div>
      <div style={{ gridColumn: "1 / -1", textAlign: "center" }}>
        <AppTypography
          component="div"
          fontWeight={700}
          style={{ color: statusColor }}
          variant="h3"
        >
          {count}
        </AppTypography>
        <AppTypography
          component="div"
          style={{ color: statusColor }}
          variant="body2"
        >
          {status}
        </AppTypography>
      </div>
    </FrostedCard>
  );
}
