import type { DiskPowerData } from "@/api";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import Chip from "@/components/ui/AppChip";
import AppTypography from "@/components/ui/AppTypography";
import { useAppTheme } from "@/theme";

interface PowerStatesTabProps {
  power: DiskPowerData;
}

type PowerStateRow = DiskPowerData["states"][number];

const powerStateColumns: AppVirtualTableColumnDef<PowerStateRow>[] = [
  {
    accessorKey: "state",
    header: "State",
  },
  {
    id: "op",
    header: "Op",
    cell: () => "+",
  },
  {
    accessorKey: "maxPowerW",
    header: "Max Power",
    cell: ({ row }) => `${row.original.maxPowerW}W`,
    meta: { align: "right" },
  },
  {
    accessorKey: "description",
    header: "Description",
    meta: {
      cellStyle: { fontSize: "0.75rem" },
    },
  },
];

export const PowerStatesTab = ({ power }: PowerStatesTabProps) => {
  const theme = useAppTheme();
  return (
    <>
      <div
        style={{
          marginBottom: theme.spacing(3),
        }}
      >
        <AppTypography gutterBottom variant="subtitle2">
          Current State
        </AppTypography>
        <div
          style={{
            display: "flex",
            gap: theme.spacing(2),
            alignItems: "center",
          }}
        >
          <Chip
            color="primary"
            label={`Power State ${power.currentState}`}
            variant="soft"
          />
          <AppTypography color="text.secondary" variant="body2">
            Estimated Power: ~{power.estimatedW.toFixed(2)}W
          </AppTypography>
        </div>
      </div>

      <AppTypography gutterBottom variant="subtitle2">
        Supported Power States
      </AppTypography>
      <AppVirtualTable
        ariaLabel="Supported drive power states"
        columns={powerStateColumns}
        data={power.states}
        density="compact"
        fillAvailable={false}
        getRowId={(state) => String(state.state)}
        maxHeight={400}
        selectedRowId={String(power.currentState)}
        variant="embedded"
      />
    </>
  );
};
