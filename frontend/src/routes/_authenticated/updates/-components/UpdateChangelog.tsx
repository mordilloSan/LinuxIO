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

  const changelog = isError
    ? "Failed to load changelog"
    : data?.changelog || "No changelog is available for this update";
  const homepage = isError ? undefined : data?.homepage;

  return (
    <div>
      <AppTypography
        component="div"
        style={{ whiteSpace: "pre-wrap" }}
        variant="body2"
      >
        {changelog}
      </AppTypography>
      {homepage ? (
        <div
          style={{ display: "flex", justifyContent: "flex-end", marginTop: 8 }}
        >
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
        </div>
      ) : null}
    </div>
  );
};

export default UpdateChangelog;
