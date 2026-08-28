import { useSuspenseQuery } from "@tanstack/react-query";
import { getRouteApi } from "@tanstack/react-router";
import { useCallback, useMemo, useState } from "react";

import { type DockerImage, linuxio, useCallMutation } from "@/api";
import DockerImageCard from "@/components/cards/DockerImageCard";
import BatchDeleteDialog from "@/components/docker/BatchDeleteDialog";
import DockerResourceDetailsLayout from "@/components/docker/DockerResourceDetailsLayout";
import ReorderableCardGrid from "@/components/reorder/ReorderableCardGrid";
import { RoutedTabSearch } from "@/components/tabbar";
import AppVirtualTable from "@/components/tables/AppVirtualTable";
import type { AppVirtualTableColumnDef } from "@/components/tables/AppVirtualTable.types";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import Chip from "@/components/ui/AppChip";
import AppHeaderSearch from "@/components/ui/AppHeaderSearch";
import AppTypography from "@/components/ui/AppTypography";
import { useFocusedResourceParam } from "@/hooks/useFocusedResourceParam";
import { useRegisterCreateHandler } from "@/hooks/useRegisterCreateHandler";
import { useReorderableSurface } from "@/hooks/useReorderableSurface";
import { useReorderableTableDnd } from "@/hooks/useReorderableTableDnd";
import { CARD_GRID_SIZE_STANDARD } from "@/theme/constants";
import {
  longTextStyles,
  responsiveTextStyles,
  wrappableChipStyle,
  wrappableChipLabelStyle,
} from "@/theme/tableStyles";
const UNTAGGED_REF = "<none>:<none>";

// Split a reference into repo/tag. The last colon only separates a tag when it
// comes after the last slash — otherwise it belongs to a registry port
// (localhost:5000/foo).
const splitImageRef = (ref: string) => {
  const colonIdx = ref.lastIndexOf(":");
  if (colonIdx > ref.lastIndexOf("/")) {
    return {
      repo: ref.slice(0, colonIdx) || "<none>",
      tag: ref.slice(colonIdx + 1) || "<none>",
    };
  }
  return { repo: ref || "<none>", tag: "<none>" };
};

const ImageDetailsContent = ({
  image,
}: {
  image: { id: string; raw: DockerImage };
}) => (
  <div className="expand-panel">
    <div>
      <AppTypography gutterBottom variant="subtitle2">
        <b>Full Image ID:</b>
      </AppTypography>
      <AppTypography
        className="expand-panel__mono"
        style={longTextStyles}
        variant="body2"
      >
        {image.id}
      </AppTypography>
    </div>

    <div>
      <AppTypography gutterBottom variant="subtitle2">
        <b>Labels:</b>
      </AppTypography>
      <div className="expand-panel__chips">
        {image.raw.Labels && Object.keys(image.raw.Labels).length > 0 ? (
          Object.entries(image.raw.Labels).map(([key, val]) => (
            <Chip
              key={key}
              label={`${key}: ${val}`}
              size="small"
              style={wrappableChipStyle}
              labelStyle={wrappableChipLabelStyle}
              variant="soft"
            />
          ))
        ) : (
          <AppTypography color="text.secondary" variant="body2">
            (no labels)
          </AppTypography>
        )}
      </div>
    </div>

    <div>
      <AppTypography gutterBottom variant="subtitle2">
        <b>Image Digests:</b>
      </AppTypography>
      <div>
        {image.raw.RepoDigests && image.raw.RepoDigests.length > 0 ? (
          image.raw.RepoDigests.map((digest) => (
            <AppTypography
              key={digest}
              className="expand-panel__mono"
              style={longTextStyles}
              variant="body2"
            >
              {digest}
            </AppTypography>
          ))
        ) : (
          <AppTypography color="text.secondary" variant="body2">
            (no digests)
          </AppTypography>
        )}
      </div>
    </div>
  </div>
);

interface ImageListProps {
  onMountCreateHandler?: (handler: () => void) => void;
  viewMode?: "table" | "card";
}
const getImageRowId = (image: { id: string }) => image.id;

const dockerRouteApi = getRouteApi("/_authenticated/docker/images");

const ImageList = ({
  onMountCreateHandler,
  viewMode = "table",
}: ImageListProps) => {
  const navigate = dockerRouteApi.useNavigate();
  const searchParams = dockerRouteApi.useSearch();
  const focusedImageId =
    typeof searchParams.image === "string" ? searchParams.image : undefined;
  const { data: rawImages } = useSuspenseQuery({
    ...linuxio.docker.list_images,
    ...{
      refetchInterval: 10000,
    },
  });
  const images = rawImages;
  const [search, setSearch] = useState("");
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);

  // Create image handler
  const handleCreateImage = useCallback(() => {
    // TODO: Open image pull/import dialog
    console.log("Add image clicked");
  }, []);

  useRegisterCreateHandler(onMountCreateHandler, handleCreateImage);

  // One row per image — an image with several tags stays a single row and
  // lists all of them, instead of appearing once per tag.
  const imageRows = useMemo(
    () =>
      images.map((img) => {
        const refs = img.RepoTags?.filter((ref) => ref !== UNTAGGED_REF) ?? [];
        const parts = (refs.length ? refs : [UNTAGGED_REF]).map(splitImageRef);
        const repos = [...new Set(parts.map((part) => part.repo))];
        // Tags alone are ambiguous once the same ID is tagged under more than
        // one repository, so show the full reference in that case.
        const tags = [
          ...new Set(
            parts.map((part) =>
              repos.length > 1 ? `${part.repo}:${part.tag}` : part.tag,
            ),
          ),
        ];
        return {
          id: img.Id,
          refs,
          repo: repos[0] ?? "<none>",
          repos,
          tags,
          shortId: img.Id?.slice(7, 19) || "",
          size: (img.Size / (1024 * 1024)).toFixed(2),
          created: new Date(img.Created * 1000).toLocaleString(),
          containers: img.Containers || 0,
          raw: img,
        };
      }),
    [images],
  );
  const surface = useReorderableSurface({
    getId: getImageRowId,
    items: imageRows,
    surface: "docker.images",
  });
  const tableDnd = useReorderableTableDnd<
    (typeof imageRows)[number],
    (typeof imageRows)[number]
  >({
    handleAriaLabel: "Reorder image",
    surface,
  });
  const orderedRows = surface.items;
  const filtered = useMemo(() => {
    const needle = search.toLowerCase();
    if (!needle) return orderedRows;
    return orderedRows.filter(
      (img) =>
        img.repos.some((repo) => repo.toLowerCase().includes(needle)) ||
        img.tags.some((tag) => tag.toLowerCase().includes(needle)) ||
        img.shortId.toLowerCase().includes(needle),
    );
  }, [orderedRows, search]);

  const clearFocusedImage = useCallback(() => {
    void navigate({
      to: "/docker/images",
      search: (previous) => ({ ...previous, image: undefined }),
    });
  }, [navigate]);
  const handleOpenImage = useCallback(
    (id: string) => {
      if (surface.editMode) return;
      void navigate({
        to: "/docker/images",
        search: (previous) => ({ ...previous, image: id }),
      });
    },
    [navigate, surface.editMode],
  );
  // Configless: this is a batch flow — the dialog owns aggregation and toasts.
  const { mutateAsync: deleteImage } = useCallMutation(
    linuxio.docker.delete_image,
  );
  const handleDeleteSuccess = () => {
    clearFocusedImage();
  };
  const focusedImage = useFocusedResourceParam({
    focusedId: focusedImageId,
    getId: getImageRowId,
    items: orderedRows,
    onClear: clearFocusedImage,
  });
  const handleImageRowClick = useCallback(
    ({ original: image }: { original: { id: string } }) =>
      handleOpenImage(image.id),
    [handleOpenImage],
  );

  // Stable column defs — see docs/table-row-gestures.md: a rebuilt array
  // remounts every cell subtree on the press that arms the reorder hold.
  const columns = useMemo<
    AppVirtualTableColumnDef<(typeof filtered)[number]>[]
  >(
    () => [
      {
        accessorKey: "repo",
        header: "Repository",
        cell: ({ row }) => (
          <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
            <AppTypography
              fontWeight={500}
              style={responsiveTextStyles}
              variant="body2"
            >
              {row.original.repo}
            </AppTypography>
            {row.original.repos.length > 1 && (
              <Chip
                label={`+${row.original.repos.length - 1}`}
                size="xsmall"
                title={row.original.repos.slice(1).join(", ")}
                variant="soft"
              />
            )}
          </div>
        ),
        meta: { align: "left" },
      },
      {
        id: "tags",
        accessorFn: (image) => image.tags.join(", "),
        header: "Tags",
        cell: ({ row }) => (
          <div style={{ display: "flex", flexWrap: "wrap", gap: 4 }}>
            {row.original.tags.map((tag) => (
              <Chip key={tag} label={tag} size="xsmall" variant="soft" />
            ))}
          </div>
        ),
        meta: {
          align: "left",
          width: "180px",
        },
      },
      {
        accessorKey: "id",
        header: "Full ID",
        cell: ({ row }) => (
          <AppTypography
            copyText={row.original.id}
            noWrap
            style={{
              ...responsiveTextStyles,
            }}
            title={row.original.id}
            variant="body2"
          >
            <span style={{ fontWeight: 700 }}>Full ID: </span>
            <AppTypography
              color="text.secondary"
              component="span"
              style={{ fontFamily: "var(--app-font-mono)" }}
              variant="caption"
            >
              {row.original.id}
            </AppTypography>
          </AppTypography>
        ),
        meta: {
          align: "left",
          hideBelow: "lg",
          width: "260px",
        },
      },
      {
        accessorKey: "size",
        header: "Size",
        cell: ({ row }) => (
          <AppTypography style={responsiveTextStyles} variant="body2">
            {row.original.size} MB
          </AppTypography>
        ),
        meta: {
          align: "right",
          width: "100px",
        },
      },
      {
        accessorKey: "created",
        header: "Created",
        cell: ({ row }) => (
          <AppTypography style={responsiveTextStyles} variant="body2">
            {row.original.created}
          </AppTypography>
        ),
        meta: {
          align: "left",
          hideBelow: "sm",
        },
      },
      {
        accessorKey: "containers",
        header: "Used By",
        cell: ({ row }) => (
          <Chip
            color={row.original.containers > 0 ? "success" : "default"}
            label={row.original.containers}
            size="small"
            style={{ minWidth: 40 }}
            variant="soft"
          />
        ),
        meta: {
          align: "center",
          width: "100px",
        },
      },
    ],
    [],
  );
  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        height: "100%",
        minHeight: 0,
      }}
    >
      {!focusedImage && (
        <RoutedTabSearch active={search !== ""}>
          <AppHeaderSearch
            clearOnDocumentEscape
            onChange={setSearch}
            placeholder="Search images…"
            value={search}
          />
        </RoutedTabSearch>
      )}
      {focusedImage ? (
        <DockerResourceDetailsLayout
          onClose={clearFocusedImage}
          resourceLabel="image"
          subtitle={`${focusedImage.size} MB · ${focusedImage.created}`}
          summary={
            <DockerImageCard
              image={focusedImage}
              actions={
                <AppActionIconButton
                  ariaLabel={`Delete image ${focusedImage.repo}`}
                  color="var(--app-palette-error-main)"
                  icon="mdi:delete"
                  iconSize={18}
                  label="Delete image"
                  onClick={() => setDeleteDialogOpen(true)}
                />
              }
              selected
            />
          }
          title={focusedImage.repo}
        >
          <ImageDetailsContent image={focusedImage} />
        </DockerResourceDetailsLayout>
      ) : viewMode === "card" ? (
        filtered.length > 0 ? (
          <ReorderableCardGrid
            fillAvailable
            getId={getImageRowId}
            items={filtered}
            renderItem={(image) => (
              <DockerImageCard
                image={image}
                onOpen={
                  surface.editMode ? undefined : () => handleOpenImage(image.id)
                }
              />
            )}
            size={CARD_GRID_SIZE_STANDARD}
            surface={surface}
          />
        ) : (
          <div
            style={{
              textAlign: "center",
              paddingTop: "var(--app-space-16)",
              paddingBottom: "var(--app-space-16)",
            }}
          >
            <AppTypography color="text.secondary" variant="body2">
              No images found.
            </AppTypography>
          </div>
        )
      ) : (
        <AppVirtualTable
          ariaLabel="Docker images"
          columns={columns}
          data={filtered}
          dnd={tableDnd}
          emptyMessage="No images found."
          fillAvailable
          getRowId={getImageRowId}
          onRowClick={surface.editMode ? undefined : handleImageRowClick}
          selectedRowId={focusedImageId ?? null}
        />
      )}

      <BatchDeleteDialog
        failureHint="(likely in use)"
        items={
          focusedImage
            ? [
                {
                  key: focusedImage.id,
                  label: focusedImage.refs[0] ?? focusedImage.shortId,
                  refs: focusedImage.refs,
                },
              ]
            : []
        }
        noun="image"
        onClose={() => setDeleteDialogOpen(false)}
        onDeleteOne={async (image) => {
          // A multi-tag image can't be removed by ID (Docker refuses without
          // --force); removing each reference untags it, and the last one
          // drops the image itself.
          for (const target of image.refs.length > 0
            ? image.refs
            : [image.key]) {
            await deleteImage({ imageId: target });
          }
        }}
        onSuccess={handleDeleteSuccess}
        open={deleteDialogOpen}
        warning="Images in use by containers cannot be deleted."
      />
    </div>
  );
};
export default ImageList;
