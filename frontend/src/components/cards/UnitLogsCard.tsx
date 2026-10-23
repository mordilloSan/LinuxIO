import { Icon } from "@iconify/react";
import { useState, type ReactNode } from "react";

import { openChannel, type Stream } from "@/api";
import FrostedCard from "@/components/cards/FrostedCard";
import AppFormControlLabel from "@/components/ui/AppFormControlLabel";
import AppSwitch from "@/components/ui/AppSwitch";
import AppTooltip from "@/components/ui/AppTooltip";
import AppTypography from "@/components/ui/AppTypography";
import LogViewport from "@/components/ui/LogViewport";
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
  liveMode: boolean;
  onLiveModeChange: (liveMode: boolean) => void;
  titleContent: ReactNode;
}

const UnitLogsLiveContent = ({
  createStream,
  liveMode,
  onLiveModeChange,
  titleContent,
}: UnitLogsLiveContentProps) => {
  const { logs, isLoading, error, logsBoxRef } = useLogStream({
    open: true,
    createStream,
    liveMode,
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
                onChange={(_, value) => onLiveModeChange(value)}
                size="small"
              />
            }
            label="Live"
          />
        </AppTooltip>
      </div>
      <LogViewport
        error={error}
        isLoading={isLoading}
        logs={logs}
        logsBoxRef={logsBoxRef}
      />
    </>
  );
};

const UnitLogsCard = ({ unitName, title, createStream }: UnitLogsCardProps) => {
  const [liveMode, setLiveMode] = useState(true);
  return (
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
        key={unitName}
        liveMode={liveMode}
        onLiveModeChange={setLiveMode}
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
};

export default UnitLogsCard;
