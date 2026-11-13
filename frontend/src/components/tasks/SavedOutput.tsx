import { useInfiniteQuery } from "@tanstack/react-query";
import { useLayoutEffect, useRef } from "react";

import {
  call,
  isConnectionLossError,
  linuxio,
  LinuxIOError,
  type TaskOutputRequest,
} from "@/api";
import AppAlert from "@/components/ui/AppAlert";
import AppButton from "@/components/ui/AppButton";
import AppTypography from "@/components/ui/AppTypography";
import LogViewport from "@/components/ui/LogViewport";

/** The most one "Show earlier" press asks for. */
const EARLIER_CHUNK_BYTES = 262144;

/** Where a chunk starts and how long it is; neither asks for the end. */
type OutputChunk = Pick<TaskOutputRequest, "offset" | "limit">;
const OUTPUT_END: OutputChunk = {};

const isNotFound = (error: unknown) =>
  error instanceof LinuxIOError && Number(error.code) === 404;

interface SavedOutputProps {
  taskId: string;
}

/**
 * The saved output of an ended job or scheduled run: its end first, with
 * earlier parts loaded on request. Key it on the task so a new run starts
 * from its own end.
 */
export default function SavedOutput({ taskId }: SavedOutputProps) {
  const logsBoxRef = useRef<HTMLDivElement>(null);
  // How far the view was from the bottom of the box when earlier text was
  // asked for; null until then, so the first text opens at the end.
  const bottomGap = useRef<number | null>(null);
  // The first page is the end of the output and each next page the part
  // before it, so a refetch, which walks forward from the first page, loads
  // every part again in order.
  const query = useInfiniteQuery({
    queryKey: [...linuxio.tasks.output({ taskId }).queryKey, "pages"],
    initialPageParam: OUTPUT_END,
    queryFn: ({ pageParam, signal }) =>
      call("tasks.output", { taskId, ...pageParam }, { signal }),
    // Always from the start the server reported, which may be later than the
    // one asked for, so each chunk ends exactly where the later one begins.
    getNextPageParam: (earliest, pages): OutputChunk | undefined => {
      if (earliest.offset === 0) return undefined;
      // A reply that did not start earlier would loop the button.
      const later = pages.at(-2);
      if (later && earliest.offset >= later.offset) return undefined;
      const offset = Math.max(0, earliest.offset - EARLIER_CHUNK_BYTES);
      return { offset, limit: earliest.offset - offset };
    },
    // An ended run's output no longer changes.
    staleTime: Infinity,
    // A run without saved output is an answer, not a fault to retry; this
    // component shows every error itself, so none should also raise a toast.
    retry: (failureCount, error) =>
      failureCount < 1 && !isNotFound(error) && !isConnectionLossError(error),
    meta: { silent: true },
  });

  // The pages run from the end backwards and the text from the earliest; a
  // part that did not start before the later one repeats it.
  const pages = query.data?.pages ?? [];
  const logs = pages
    .filter((page, index) => {
      const later = pages[index - 1];
      return !later || page.offset < later.offset;
    })
    .map((page) => page.content)
    .reverse()
    .join("");

  // The scroll position lives in the DOM, outside React: open at the end, and
  // when text is added above keep what was in view where it was.
  useLayoutEffect(() => {
    const box = logsBoxRef.current;
    if (!box || logs === "") return;
    box.scrollTop = box.scrollHeight - (bottomGap.current ?? box.clientHeight);
    bottomGap.current = null;
  }, [logs]);

  const showEarlier = () => {
    const box = logsBoxRef.current;
    if (box) bottomGap.current = box.scrollHeight - box.scrollTop;
    void query.fetchNextPage();
  };

  if (!query.data) {
    if (!query.isError) {
      return (
        <LogViewport error={null} isLoading logs="" logsBoxRef={logsBoxRef} />
      );
    }
    return isNotFound(query.error) ? (
      <AppTypography color="text.secondary">
        No output was saved for this run.
      </AppTypography>
    ) : (
      <AppAlert severity="error">
        Unable to load the output: {query.error.message}
      </AppAlert>
    );
  }

  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        gap: "var(--app-space-8)",
      }}
    >
      {query.hasNextPage && (
        <div>
          <AppButton
            disabled={query.isFetchingNextPage}
            onClick={showEarlier}
            size="small"
          >
            Show earlier
          </AppButton>
        </div>
      )}
      {query.isError && (
        <AppTypography color="error">
          Unable to load earlier output: {query.error.message}
        </AppTypography>
      )}
      <LogViewport
        error={null}
        isLoading={false}
        logs={logs}
        logsBoxRef={logsBoxRef}
      />
    </div>
  );
}
