import { Icon } from "@iconify/react";
import type { ReactNode } from "react";

import { openChannel, type Stream } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import { useLogStream } from "@/hooks/useLogStream";
import { CARD_PADDING_LG } from "@/theme/constants";

interface UnitLogsCardProps {
  /** Stream factory; defaults to the systemd service log stream for `unitName`. */
  createStream?: (tail: string) => Stream | null;
  title: string;
  unitName?: string;
}

interface UnitLogsLiveContentProps {
  createStream: (tail: string) => Stream | null;
  titleContent: ReactNode;
}

const UnitLogsLiveContent = ({
  createStream,
  titleContent,
}: UnitLogsLiveContentProps) => {
  const { logs, isLoading, error, liveMode, setLiveMode, logsBoxRef } =
    useLogStream({
      open: true,
      createStream,
    });

  return (
    <>
      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "center",
          marginBottom: 12,
        }}
      >
        {titleContent}
        <AppTooltip
          title={liveMode ? "Live streaming ON" : "Live streaming OFF"}
        >
          <AppFormControlLabel
            control={
              <AppSwitch
                checked={liveMode}
                onChange={(_, value) => setLiveMode(value)}
                size="small"
              />
            }
            label="Live"
          />
        </AppTooltip>
      </div>
      <div
        style={{
          position: "relative",
          display: "flex",
          flexDirection: "column",
          // Fills whatever height the card is given, and falls back to the
          // viewport's own bounds when the card is content-sized.
          flex: 1,
          minHeight: 0,
          backgroundColor: "var(--app-code-block-background)",
          color: "var(--app-code-block-color)",
          borderRadius: 4,
          overflow: "hidden",
        }}
      >
        {isLoading && (
          <div
            style={{
              position: "absolute",
              inset: 0,
              background:
                "color-mix(in srgb, var(--app-code-block-background), transparent 15%)",
              zIndex: 10,
            }}
          >
            <ComponentLoader />
          </div>
        )}
        {error && (
          <div style={{ color: "var(--app-palette-error-main)", padding: 16 }}>
            {error}
          </div>
        )}
        <AppTypography
          component="div"
          ref={logsBoxRef}
          style={{
            flex: 1,
            padding: 16,
            overflow: "auto",
            fontFamily: "Fira Mono, monospace",
            whiteSpace: "pre-wrap",
            wordBreak: "break-all",
            minHeight: 120,
            maxHeight: 340,
          }}
          variant="body2"
        >
          {!isLoading &&
            !error &&
            (logs || (
              <AppTypography
                color="text.secondary"
                component="span"
                variant="caption"
              >
                No logs available.
              </AppTypography>
            ))}
        </AppTypography>
      </div>
    </>
  );
};

const UnitLogsCard = ({ unitName, title, createStream }: UnitLogsCardProps) => (
  <FrostedCard
    style={{
      boxSizing: "border-box",
      display: "flex",
      flexDirection: "column",
      // Stretches to a grid row that sizes the card; a no-op where the card is
      // a full-width bottom panel with no height to inherit.
      height: "100%",
      padding: CARD_PADDING_LG,
    }}
  >
    <UnitLogsLiveContent
      createStream={
        createStream ??
        ((tail) =>
          openChannel("logs.service.follow", {
            serviceName: unitName ?? "",
            lines: tail,
          }))
      }
      titleContent={
        <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
          <Icon
            color="var(--app-palette-text-secondary)"
            height={20}
            icon="mdi:console"
            width={20}
          />
          <AppTypography component="span" fontWeight={600} variant="body2">
            {title}
          </AppTypography>
        </div>
      }
    />
  </FrostedCard>
);

export default UnitLogsCard;
