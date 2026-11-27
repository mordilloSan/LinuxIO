import { useQuery } from "@tanstack/react-query";

import { CACHE_TTL_MS, linuxio } from "@/api";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppLinkButton from "@/components/ui/AppLinkButton";
import AppTypography from "@/components/ui/AppTypography";

/** The focused update's changelog, with the project's page when it has none. */
const UpdateChangelog = ({ packageId }: { packageId: string }) => {
  const { data, isError, isLoading } = useQuery({
    ...linuxio.updates.get_update_detail({ packageId }),
    staleTime: CACHE_TTL_MS.FIVE_MINUTES,
  });

  if (isLoading) {
    return (
      <div
        style={{
          display: "flex",
          justifyContent: "center",
          paddingBottom: 16,
          paddingTop: 16,
        }}
      >
        <AppCircularProgress aria-label="Loading changelog" size={20} />
      </div>
    );
  }

  const changelog = isError ? undefined : data?.changelog;
  if (changelog) {
    return (
      <AppTypography
        component="div"
        style={{ whiteSpace: "pre-wrap" }}
        variant="body2"
      >
        {changelog}
      </AppTypography>
    );
  }

  // With nothing to read, the note and the project's page sit on one row at
  // the foot of the panel.
  const homepage = isError ? undefined : data?.homepage;
  return (
    <div
      style={{
        alignItems: "center",
        display: "flex",
        gap: 8,
        justifyContent: "space-between",
        marginTop: "auto",
      }}
    >
      <AppTypography component="div" variant="body2">
        {isError
          ? "Failed to load changelog"
          : "No changelog is available for this update"}
      </AppTypography>
      {homepage ? (
        <AppLinkButton
          href={homepage}
          rel="noopener noreferrer"
          size="small"
          target="_blank"
          title={homepage}
          variant="text"
        >
          Project page
        </AppLinkButton>
      ) : null}
    </div>
  );
};

export default UpdateChangelog;
