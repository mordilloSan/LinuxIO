import { useState } from "react";

import type { TaskSnapshot } from "@/api";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import { taskTitle } from "@/tasks/taskTitles";
import { formatDate } from "@/utils/formatters";

import { useJobHostStatus } from "./useJobHostStatus";

const waitingText = (running: number) => {
  if (running === 0) return "LinuxIO was updated. New jobs can start shortly.";
  const jobs = running === 1 ? "1 running job" : `${running} running jobs`;
  return `LinuxIO was updated. New jobs wait for ${jobs} to finish.`;
};

/** "43%" when the job's last progress has a percentage. */
const progressText = (job: TaskSnapshot) => {
  const percentage = job.progress?.percentage;
  return typeof percentage === "number" && Number.isFinite(percentage)
    ? `${Math.round(percentage)}%`
    : undefined;
};

/**
 * Tells every signed-in user that a LinuxIO update waits for running jobs to
 * finish before new jobs can start, and which jobs those are.
 */
const JobsUpdateBanner = () => {
  const { updatePending, running, jobs } = useJobHostStatus();
  const [expanded, setExpanded] = useState(false);

  if (!updatePending) return null;

  const showList = expanded && jobs.length > 0;

  return (
    <AppAlert
      action={
        jobs.length > 0 ? (
          <AppButton
            aria-expanded={showList}
            onClick={() => setExpanded((open) => !open)}
            size="small"
          >
            {showList ? "Hide running jobs" : "Show running jobs"}
          </AppButton>
        ) : undefined
      }
      // Not an alert: the text changes with each poll, and a screen reader
      // should not be interrupted every time.
      role="status"
      severity="info"
      style={{
        borderRadius: "var(--app-radius-card)",
        marginBottom: "var(--app-space-4)",
      }}
    >
      <div>{waitingText(running)}</div>
      {showList && (
        // Many jobs scroll inside the banner rather than push the page down.
        <ul
          style={{
            margin: "var(--app-space-4) 0 0",
            maxHeight: "min(25vh, 240px)",
            overflowY: "auto",
            overscrollBehavior: "contain",
            paddingLeft: "var(--app-space-16)",
          }}
        >
          {jobs.map((job) => {
            const details = [
              job.owner?.username,
              job.started_at && `started ${formatDate(job.started_at)}`,
              progressText(job),
            ].filter(Boolean);
            return (
              <li key={job.id}>{[taskTitle(job), ...details].join(" · ")}</li>
            );
          })}
        </ul>
      )}
    </AppAlert>
  );
};

export default JobsUpdateBanner;
