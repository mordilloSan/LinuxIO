import type { Process } from "@/api";

export type ProcessView = "processes" | "programs";

export interface ProcessSearch {
  filter?: string;
  view?: Extract<ProcessView, "programs">;
}

export const validateProcessSearch = (
  search: Record<string, unknown>,
): ProcessSearch => ({
  ...("filter" in search && typeof search.filter === "string" && search.filter
    ? { filter: search.filter }
    : {}),
  ...(search.view === "programs" ? { view: "programs" } : {}),
});

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
