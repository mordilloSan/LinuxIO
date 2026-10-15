import type { Process } from "@/api";

export type ProcessView = "processes" | "programs";

export const filterProcessRows = (
  rows: Process[],
  filter: string,
): Process[] => {
  const query = filter.trim().toLocaleLowerCase();
  if (!query) return rows;
  return rows.filter((row) =>
    [row.name, row.username ?? "", (row.cmdline ?? []).join(" ")].some(
      (field) => field.toLocaleLowerCase().includes(query),
    ),
  );
};
