export interface FTSStatus {
  detail: string;
  label: string;
}

export function getFTSStatus(
  ftsSearchEnabled: boolean,
  ftsActive: boolean,
): FTSStatus {
  if (ftsSearchEnabled && ftsActive) {
    return {
      label: "Fast search enabled",
      detail:
        "Full-text search is active. Full indexes take more time and storage.",
    };
  }
  if (!ftsSearchEnabled && !ftsActive) {
    return {
      label: "Fast indexing enabled",
      detail: "Full indexes are faster and smaller; searches use the fallback.",
    };
  }
  if (ftsSearchEnabled) {
    return {
      label: "Fast search pending",
      detail: "Run a full index to activate full-text search.",
    };
  }
  return {
    label: "Fast indexing pending",
    detail: "Run a full index to switch to a faster, smaller index.",
  };
}
