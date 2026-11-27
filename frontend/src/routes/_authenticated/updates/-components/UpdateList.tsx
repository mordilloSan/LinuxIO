import { noop, useQueryClient } from "@tanstack/react-query";
import { getRouteApi } from "@tanstack/react-router";

import { CACHE_TTL_MS, linuxio, type Update } from "@/api";
import ResourceDetailsLayout from "@/components/cards/ResourceDetailsLayout";
import ReorderableCardGrid from "@/components/reorder/ReorderableCardGrid";
import { useReorderableSurface } from "@/components/reorder/useReorderableSurface";
import AppTypography from "@/components/ui/AppTypography";
import { useMediaQuery } from "@/hooks/useMediaQuery";
import { useFocusedResourceParam } from "@/routes/_authenticated/docker/-hooks/useFocusedResourceParam";
import { up } from "@/theme/breakpoints";
import { DASHBOARD_CARD_SPACING, GAP_XS } from "@/theme/constants";

import UpdateCard from "./UpdateCard";
import UpdateChangelog from "./UpdateChangelog";

const updatesRouteApi = getRouteApi("/_authenticated/updates/");

interface Props {
  isUpdating?: boolean;
  onUpdateClick: (pkg: string) => void;
  updates: Update[];
}
const getUpdateId = (update: Update) => update.package_id;

const GRID_COLUMNS = { xs: 1, sm: 2, md: 3, lg: 4, xl: 5 } as const;
// ReorderableCardGrid's default gap between cards.
const GRID_GAP = DASHBOARD_CARD_SPACING * GAP_XS;

const UpdateList = ({ updates, onUpdateClick, isUpdating }: Props) => {
  const queryClient = useQueryClient();
  const navigate = updatesRouteApi.useNavigate();
  const { package: focusedPackageId } = updatesRouteApi.useSearch();
  const isXl = useMediaQuery(up("xl"));
  const isLg = useMediaQuery(up("lg"));
  const surface = useReorderableSurface({
    getId: getUpdateId,
    items: updates,
    surface: "updates.list",
  });
  const focusPackage = (packageId: string | null) => {
    void navigate({
      search: (previous) => ({ ...previous, package: packageId ?? undefined }),
      to: "/updates",
    });
  };
  // The details close by themselves when the update leaves the list, as it
  // does once it is installed.
  const focusedUpdate = useFocusedResourceParam({
    focusedId: focusedPackageId,
    getId: getUpdateId,
    items: updates,
    onClear: () => focusPackage(null),
  });
  const prefetchChangelog = (packageId: string) => {
    void queryClient
      .query({
        ...linuxio.updates.get_update_detail({ packageId }),
        meta: { silent: true },
        staleTime: CACHE_TTL_MS.FIVE_MINUTES,
      })
      .catch(noop);
  };
  if (!updates.length && !isUpdating) {
    return (
      <div
        style={{
          textAlign: "left",
        }}
      >
        <AppTypography variant="h6">Your system is up to date </AppTypography>
      </div>
    );
  }
  if (isUpdating) {
    return null; // Hide list while updating; only the progress bar should show
  }
  if (focusedUpdate) {
    // The focused card keeps the width of a card in the grid; below md the
    // layout stacks it above the panel at full width.
    const columns = isXl
      ? GRID_COLUMNS.xl
      : isLg
        ? GRID_COLUMNS.lg
        : GRID_COLUMNS.md;
    return (
      <ResourceDetailsLayout
        onClose={() => focusPackage(null)}
        resourceLabel="update"
        summary={
          // Its own height, not the changelog panel's.
          <div style={{ alignSelf: "flex-start", width: "100%" }}>
            <UpdateCard
              onUpdate={() => onUpdateClick(focusedUpdate.package_id)}
              selected
              update={focusedUpdate}
            />
          </div>
        }
        summaryWidth={`calc((100% - ${(columns - 1) * GRID_GAP}px) / ${columns})`}
        title="Changelog"
        titleVariant="subtitle1"
      >
        <UpdateChangelog packageId={focusedUpdate.package_id} />
      </ResourceDetailsLayout>
    );
  }
  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        flex: "1 1 auto",
        minHeight: 0,
      }}
    >
      <ReorderableCardGrid
        columns={GRID_COLUMNS}
        fillAvailable
        getId={getUpdateId}
        renderItem={(update) => (
          <UpdateCard
            onOpen={
              surface.editMode
                ? undefined
                : () => focusPackage(update.package_id)
            }
            onPrefetchChangelog={() => prefetchChangelog(update.package_id)}
            update={update}
          />
        )}
        surface={surface}
      />
    </div>
  );
};
export default UpdateList;
