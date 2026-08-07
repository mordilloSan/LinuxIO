import type { Socket } from "@/api";
import Chip from "@/components/ui/AppChip";
import { AppTableCell } from "@/components/ui/AppTable";
import { useAppTheme } from "@/theme";

import UnitStatusDot from "./UnitStatusDot";
import { MobileExpandedDetails, UnitTableView } from "./UnitViews";

interface SocketTableViewProps {
  onDoubleClick?: (name: string) => void;
  onSelect?: (name: string | null) => void;
  selected?: string | null;
  sockets: Socket[];
}

const desktopColumns = [
  {
    field: "status",
    headerName: "Status",
    align: "left" as const,
    width: "120px",
    style: { paddingLeft: 8 },
  },
  { field: "name", headerName: "Name", align: "left" as const, width: "220px" },
  { field: "listen", headerName: "Listen", align: "left" as const },
  {
    field: "connections",
    headerName: "Connections",
    align: "right" as const,
    width: "130px",
  },
  {
    field: "accepted",
    headerName: "Accepted",
    align: "right" as const,
    width: "120px",
  },
];

const mobileColumns = [
  {
    field: "status",
    headerName: "Status",
    align: "left" as const,
    width: "110px",
    style: { paddingLeft: 8 },
  },
  { field: "name", headerName: "Name", align: "left" as const },
];

const SocketTableView = ({
  sockets,
  selected,
  onSelect,
  onDoubleClick,
}: SocketTableViewProps) => {
  const theme = useAppTheme();

  return (
    <UnitTableView
      data={sockets}
      desktopColumns={desktopColumns}
      emptyMessage="No sockets found."
      getRowKey={(socket) => socket.name}
      mobileColumns={mobileColumns}
      onDoubleClick={(key) => {
        if (typeof key === "string") {
          onDoubleClick?.(key);
        }
      }}
      onSelect={(key) => onSelect?.(typeof key === "string" ? key : null)}
      renderMainRow={(socket, isMobile) => (
        <>
          <AppTableCell style={{ paddingLeft: 8 }}>
            <UnitStatusDot activeState={socket.active_state} />
            {socket.active_state}
          </AppTableCell>
          <AppTableCell>{socket.name}</AppTableCell>
          {!isMobile && (
            <>
              <AppTableCell>
                <div
                  style={{
                    display: "flex",
                    gap: theme.spacing(0.5),
                    flexWrap: "wrap",
                  }}
                >
                  {socket.listen.length > 0
                    ? socket.listen.map((addr) => (
                        <Chip
                          key={addr}
                          label={addr}
                          size="small"
                          variant="soft"
                        />
                      ))
                    : "—"}
                </div>
              </AppTableCell>
              <AppTableCell align="right">{socket.n_connections}</AppTableCell>
              <AppTableCell align="right">{socket.n_accepted}</AppTableCell>
            </>
          )}
        </>
      )}
      renderMobileExpandedContent={(socket) => (
        <MobileExpandedDetails
          rows={[
            { label: "Listen", value: socket.listen.join(", ") || "—" },
            { label: "Connections", value: String(socket.n_connections) },
            { label: "Accepted", value: String(socket.n_accepted) },
          ]}
        />
      )}
      selected={selected}
    />
  );
};

export default SocketTableView;
