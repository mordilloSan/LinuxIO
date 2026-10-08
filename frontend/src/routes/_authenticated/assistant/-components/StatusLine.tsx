import { useEffect, useState } from "react";

import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppTypography from "@/components/ui/AppTypography";

import { useElapsed } from "./useElapsed";

const VERBS = [
  "Thinking",
  "Working",
  "Reading",
  "Checking",
  "Pondering",
  "Reviewing",
];

/** Rendered only while a turn runs, so its intervals never tick while idle. */
export default function StatusLine({
  turnStartedAt,
}: {
  turnStartedAt: number | null;
}) {
  const [verb, setVerb] = useState(0);
  const elapsed = useElapsed(turnStartedAt);
  useEffect(() => {
    const id = setInterval(() => setVerb((v) => (v + 1) % VERBS.length), 4000);
    return () => clearInterval(id);
  }, []);

  return (
    <div className="assistant__status">
      <AppCircularProgress size={14} />
      <AppTypography color="text.secondary" variant="caption">
        {VERBS[verb]}…
      </AppTypography>
      <AppTypography color="text.secondary" variant="caption">
        {elapsed}
      </AppTypography>
    </div>
  );
}
