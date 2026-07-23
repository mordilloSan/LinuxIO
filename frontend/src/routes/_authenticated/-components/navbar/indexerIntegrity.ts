import type { IndexerIntegrityCheck } from "@/api";

export interface IntegrityCheckOption {
  description: string;
  label: string;
  value: IndexerIntegrityCheck;
}

const FULL_INTEGRITY_CHECK_OPTION: IntegrityCheckOption = {
  value: "full",
  label: "Full",
  description:
    "Runs SQLite's complete integrity check. Most thorough, but slowest on large databases.",
};

export const INDEXER_INTEGRITY_CHECK_OPTIONS: readonly IntegrityCheckOption[] =
  [
    FULL_INTEGRITY_CHECK_OPTION,
    {
      value: "quick",
      label: "Quick",
      description:
        "Runs SQLite's faster quick check with reduced corruption coverage.",
    },
    {
      value: "off",
      label: "Off",
      description:
        "Skips the database integrity check. Fastest, but corruption may go unnoticed.",
    },
  ];

export function getIntegrityCheckOption(
  mode: string | null | undefined,
): IntegrityCheckOption {
  return (
    INDEXER_INTEGRITY_CHECK_OPTIONS.find((option) => option.value === mode) ??
    FULL_INTEGRITY_CHECK_OPTION
  );
}
