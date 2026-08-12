import { Icon } from "@iconify/react";
import type { ReactNode } from "react";

import { openChannel, type Stream } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import ComponentLoader from "@/components/loaders/ComponentLoader";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTooltip from "@/components/ui/AppTooltip";
import { useLogStream } from "@/hooks/useLogStream";
import { useAppTheme } from "@/theme";
import { alpha } from "@/utils/color";

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
  const theme = useAppTheme();
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
          backgroundColor: theme.codeBlock.background,
          color: theme.codeBlock.color,
          borderRadius: 4,
          overflow: "hidden",
        }}
      >
        {isLoading && (
          <div
            style={{
              position: "absolute",
              inset: 0,
              background: alpha(theme.codeBlock.background, 0.85),
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
        <div
          className="custom-scrollbar"
          ref={logsBoxRef}
          style={{
            padding: 16,
            overflow: "auto",
            fontFamily: "Fira Mono, monospace",
            fontSize: "0.8rem",
            whiteSpace: "pre-wrap",
            wordBreak: "break-all",
            minHeight: 120,
            maxHeight: 340,
          }}
        >
          {!isLoading &&
            !error &&
            (logs || (
              <span
                style={{
                  color: "var(--app-palette-text-secondary)",
                  fontSize: "0.75rem",
                }}
              >
                No logs available.
              </span>
            ))}
        </div>
      </div>
    </>
  );
};

const UnitLogsCard = ({ unitName, title, createStream }: UnitLogsCardProps) => (
  <FrostedCard style={{ padding: 12 }}>
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
          <span style={{ fontSize: "0.875rem", fontWeight: 600 }}>{title}</span>
        </div>
      }
    />
  </FrostedCard>
);

export default UnitLogsCard;
