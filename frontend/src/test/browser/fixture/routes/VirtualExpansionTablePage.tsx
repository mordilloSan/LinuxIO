import { useState } from "react";

import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import AppButton from "@/components/ui/AppButton";

interface FixtureRow {
  id: string;
  name: string;
  status: string;
}

const rows: FixtureRow[] = Array.from({ length: 180 }, (_, index) => ({
  id: `virtual-row-${index}`,
  name: `Virtual row ${index}`,
  status: index % 3 === 0 ? "ready" : index % 3 === 1 ? "running" : "idle",
}));

const columns: AppVirtualTableColumnDef<FixtureRow>[] = [
  {
    accessorKey: "name",
    header: "Name",
    meta: { width: "1fr" },
  },
  {
    accessorKey: "status",
    header: "Status",
    meta: { width: 140 },
  },
];

const nestedRows = [
  { id: "nested-a", label: "Nested detail A" },
  { id: "nested-b", label: "Nested detail B" },
];

export default function VirtualExpansionTablePage() {
  const [grownRows, setGrownRows] = useState<Set<string>>(() => new Set());

  return (
    <main
      data-testid="virtual-expansion-fixture"
      style={{
        boxSizing: "border-box",
        height: "100dvh",
        padding: 24,
        overflow: "hidden",
      }}
    >
      <h1>Virtual expansion fixture</h1>
      <p>180 deterministic rows exercise native detail virtualization.</p>
      <div
        data-testid="virtual-expansion-scrollport"
        style={{ height: 520, minHeight: 0, width: "100%" }}
      >
        <AppVirtualTable
          ariaLabel="Virtual expansion table"
          columns={columns}
          data={rows}
          fillAvailable={false}
          getRowAttributes={(row) => ({ id: row.id })}
          getRowCanExpand={() => true}
          getRowId={(row) => row.id}
          height="100%"
          overscan={2}
          renderExpandedContent={({ original }) => {
            const isGrown = grownRows.has(original.id);
            return (
              <div
                data-testid={`detail-${original.id}`}
                style={{ padding: "16px 20px", minHeight: isGrown ? 190 : 72 }}
              >
                <div>{`Details for ${original.name}`}</div>
                <AppButton
                  onClick={() =>
                    setGrownRows((current) => {
                      const next = new Set(current);
                      if (next.has(original.id)) next.delete(original.id);
                      else next.add(original.id);
                      return next;
                    })
                  }
                >
                  {isGrown ? "Shrink detail" : "Grow detail"}
                </AppButton>
                {original.id === rows[1].id && (
                  <AppVirtualTable
                    ariaLabel="Nested detail table"
                    columns={[
                      {
                        accessorKey: "label",
                        header: "Nested item",
                      },
                    ]}
                    data={nestedRows}
                    fillAvailable={false}
                    getRowId={(row) => row.id}
                    height={120}
                    showHeader={false}
                    variant="embedded"
                  />
                )}
              </div>
            );
          }}
        />
      </div>
    </main>
  );
}
