import { useState } from "react";
import { describe, expect, it } from "vitest";

import AppDataTable from "@/components/tables/AppDataTable";
import type { AppDataTableColumnDef } from "@/components/tables/AppDataTable";
import { render, screen } from "@/test/render";

interface SelectableRow {
  id: string;
  name: string;
}

const rows: SelectableRow[] = [{ id: "bridge", name: "bridge" }];

function SelectableTable() {
  const [selected, setSelected] = useState<Set<string>>(() => new Set());
  const columns: AppDataTableColumnDef<SelectableRow>[] = [
    {
      id: "select",
      header: "Select",
      cell: ({ row }) => (
        <input
          aria-label={`Select ${row.original.name}`}
          checked={selected.has(row.original.id)}
          onChange={(event) => {
            setSelected((current) => {
              const next = new Set(current);
              if (event.target.checked) {
                next.add(row.original.id);
              } else {
                next.delete(row.original.id);
              }
              return next;
            });
          }}
          type="checkbox"
        />
      ),
      meta: {
        getCellRenderKey: (row) => {
          const item = row as SelectableRow;
          return [item.id, selected.has(item.id)];
        },
      },
    },
    {
      accessorKey: "name",
      header: "Name",
    },
  ];

  return (
    <AppDataTable
      ariaLabel="Selectable rows"
      columns={columns}
      data={rows}
      getRowId={(row) => row.id}
    />
  );
}

describe("AppDataTable", () => {
  it("rerenders memoized cells when their render key changes", async () => {
    const { user } = render(<SelectableTable />);
    const checkbox = screen.getByRole("checkbox", { name: "Select bridge" });

    expect(checkbox).not.toBeChecked();
    await user.click(checkbox);
    expect(checkbox).toBeChecked();
    await user.click(checkbox);
    expect(checkbox).not.toBeChecked();
  });
});
