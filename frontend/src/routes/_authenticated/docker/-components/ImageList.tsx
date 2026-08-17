import { useSuspenseQuery } from "@tanstack/react-query";
import { useCallback, useMemo, useState } from "react";

import { linuxio, useCallMutation } from "@/api";
import DockerImageCard from "@/components/cards/DockerImageCard";
import GeneralDialog from "@/components/dialog/GeneralDialog";
import ReorderableCardGrid from "@/components/reorder/ReorderableCardGrid";
import { RoutedTabActions, RoutedTabSearch } from "@/components/tabbar";
import AppDataTable from "@/components/tables/AppDataTable";
import type { AppDataTableColumnDef } from "@/components/tables/AppDataTable";
import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppButton from "@/components/ui/AppButton";
import Chip from "@/components/ui/AppChip";
import {
  AppDialogActions,
  AppDialogContent,
  AppDialogContentText,
  AppDialogTitle,
} from "@/components/ui/AppDialog";
import AppHeaderSearch from "@/components/ui/AppHeaderSearch";
import AppTypography from "@/components/ui/AppTypography";
import { useCardSelectionEscape } from "@/hooks/useCardSelectionEscape";
import { useRegisterCreateHandler } from "@/hooks/useRegisterCreateHandler";
import { useReorderableSurface } from "@/hooks/useReorderableSurface";
import { useReorderableTableDnd } from "@/hooks/useReorderableTableDnd";
import { useScopedToast } from "@/hooks/useScopedToast";
import { useAppTheme } from "@/theme";
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

interface ImageListProps {
  onMountCreateHandler?: (handler: () => void) => void;
  viewMode?: "table" | "card";
}
interface DeleteImageTarget {
  id: string;
  label: string;
  refs: string[];
}
interface DeleteImageDialogProps {
  images: DeleteImageTarget[];
  onClose: () => void;
  onSuccess: () => void;
  open: boolean;
}
const DeleteImageDialog = ({
  open,
  onClose,
  images,
  onSuccess,
}: DeleteImageDialogProps) => {
  const theme = useAppTheme();
  const toast = useScopedToast({ label: "Open Docker", to: "/docker" });
  // Configless: this is a batch flow — the caller owns aggregation and toasts.
  const { mutateAsync: deleteImage, isPending: isDeleting } = useCallMutation(
    linuxio.docker.delete_image,
  );
  const handleDelete = async () => {
    // Delete images sequentially
    const failures: string[] = [];
    for (const image of images) {
      // A multi-tag image can't be removed by ID (Docker refuses without
      // --force); removing each reference untags it, and the last one drops
      // the image itself.
      const targets = image.refs.length > 0 ? image.refs : [image.id];
      try {
        for (const target of targets) {
          await deleteImage({ imageId: target });
        }
      } catch {
        failures.push(image.label);
      }
    }
    if (failures.length > 0) {
      toast.error(
        `Failed to delete ${failures.length} of ${images.length} image${images.length === 1 ? "" : "s"} (likely in use)`,
      );
    } else {
      const successMessage =
        images.length === 1
          ? `Image "${images[0].label}" deleted successfully`
          : `${images.length} images deleted successfully`;
      toast.success(successMessage);
    }
    onSuccess();
    handleClose();
  };
  const handleClose = () => {
    onClose();
  };
  return (
    <GeneralDialog fullWidth maxWidth="sm" onClose={handleClose} open={open}>
      <AppDialogTitle>
        Delete Image{images.length > 1 ? "s" : ""}
      </AppDialogTitle>
      <AppDialogContent>
        <AppDialogContentText>
          Are you sure you want to delete the following image
          {images.length > 1 ? "s" : ""}?
        </AppDialogContentText>
        <div
          style={{
            display: "flex",
            flexWrap: "wrap",
            marginTop: theme.spacing(2),
            marginBottom: theme.spacing(1),
          }}
        >
          {images.map((image) => (
            <Chip
              key={image.id}
              label={image.label}
              size="small"
              style={{
                marginRight: 4,
                marginBottom: 4,
              }}
              variant="soft"
            />
          ))}
        </div>
        <AppDialogContentText
          style={{
            marginTop: 8,
            color: "var(--app-palette-warning-main)",
          }}
        >
          This action cannot be undone. Images in use by containers cannot be
          deleted.
        </AppDialogContentText>
      </AppDialogContent>
      <AppDialogActions>
        <AppButton disabled={isDeleting} onClick={handleClose}>
          Cancel
        </AppButton>
        <AppButton
          color="error"
          disabled={isDeleting}
          onClick={handleDelete}
          variant="contained"
        >
          {isDeleting ? "Deleting..." : "Delete"}
        </AppButton>
      </AppDialogActions>
    </GeneralDialog>
  );
};
const getImageRowId = (image: { id: string }) => image.id;

// A press in layout mode belongs to the drag, not to selecting the image.
const noopSelect = () => {};

const ImageList = ({
  onMountCreateHandler,
  viewMode = "table",
}: ImageListProps) => {
  const theme = useAppTheme();
  const { data: rawImages } = useSuspenseQuery({
    ...linuxio.docker.list_images,
    ...{
      refetchInterval: 10000,
    },
  });
  const images = rawImages;
  const [search, setSearch] = useState("");
  const [selected, setSelected] = useState<Set<string>>(new Set());
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
  useCardSelectionEscape({
    enabled: viewMode === "card" && (selected.size > 0 || surface.editMode),
    isReordering: surface.editMode,
    onClearSelection: () => setSelected(new Set()),
    onExitReordering: surface.exitEditMode,
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

  // Compute effective selection - only include items that are in the filtered list
  const effectiveSelected = useMemo(() => {
    const filteredIds = new Set(filtered.map((img) => img.id));
    const result = new Set<string>();
    selected.forEach((id) => {
      if (filteredIds.has(id)) {
        result.add(id);
      }
    });
    return result;
  }, [selected, filtered]);

  const handleSelectOne = (id: string, checked: boolean) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (checked) {
        next.add(id);
      } else {
        next.delete(id);
      }
      return next;
    });
  };
  const handleDeleteSuccess = () => {
    setSelected(new Set());
  };
  const selectedImages = filtered.filter((img) =>
    effectiveSelected.has(img.id),
  );
  const columns: AppDataTableColumnDef<(typeof filtered)[number]>[] = [
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
              size="small"
              style={{ fontSize: "0.68rem" }}
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
            <Chip
              key={tag}
              label={tag}
              size="small"
              style={{ fontSize: "0.75rem" }}
              variant="soft"
            />
          ))}
        </div>
      ),
      meta: {
        align: "left",
        width: "180px",
      },
    },
    {
      accessorKey: "shortId",
      header: "Image ID",
      cell: ({ row }) => (
        <AppTypography
          style={{
            fontFamily: "var(--app-font-mono)",
            ...responsiveTextStyles,
          }}
          variant="body2"
        >
          {row.original.shortId}
        </AppTypography>
      ),
      meta: {
        align: "left",
        hideBelow: "md",
        width: "140px",
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
        <AppTypography
          style={{
            fontSize: "0.85rem",
            ...responsiveTextStyles,
          }}
          variant="body2"
        >
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
  ];
  return (
    <div
      style={{
        display: "flex",
        flexDirection: "column",
        height: "100%",
        minHeight: 0,
      }}
    >
      <RoutedTabSearch>
        <AppHeaderSearch
          onChange={setSearch}
          placeholder="Search images…"
          value={search}
        />
      </RoutedTabSearch>
      <RoutedTabActions>
        {effectiveSelected.size > 0 && (
          <AppActionIconButton
            ariaLabel={`Delete ${effectiveSelected.size} selected image${effectiveSelected.size === 1 ? "" : "s"}`}
            color={theme.palette.error.main}
            icon="mdi:delete"
            iconSize={20}
            label={`Delete ${effectiveSelected.size} selected image${effectiveSelected.size === 1 ? "" : "s"}`}
            onClick={() => setDeleteDialogOpen(true)}
          />
        )}
      </RoutedTabActions>
      {viewMode === "card" ? (
        filtered.length > 0 ? (
          <ReorderableCardGrid
            fillAvailable
            getId={getImageRowId}
            items={filtered}
            renderItem={(image) => (
              <DockerImageCard
                image={image}
                onSelect={
                  surface.editMode
                    ? noopSelect
                    : (checked) => handleSelectOne(image.id, checked)
                }
                selected={effectiveSelected.has(image.id)}
              />
            )}
            size={CARD_GRID_SIZE_STANDARD}
            surface={surface}
          />
        ) : (
          <div
            style={{
              textAlign: "center",
              paddingTop: theme.spacing(4),
              paddingBottom: theme.spacing(4),
            }}
          >
            <AppTypography color="text.secondary" variant="body2">
              No images found.
            </AppTypography>
          </div>
        )
      ) : (
        <AppDataTable
          ariaLabel="Docker images"
          columns={columns}
          data={filtered}
          dnd={tableDnd}
          emptyMessage="No images found."
          fillAvailable
          getRowId={(image) => image.id}
          onSelectAll={(rowIds) => setSelected(new Set(rowIds))}
          selectedRowIds={effectiveSelected}
          onClearSelection={() => setSelected(new Set())}
          onRowDoubleClick={({ original: image }) =>
            handleSelectOne(image.id, !effectiveSelected.has(image.id))
          }
          renderExpandedContent={({ original: image }) => (
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
                  {image.raw.Labels &&
                  Object.keys(image.raw.Labels).length > 0 ? (
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
          )}
        />
      )}

      <DeleteImageDialog
        images={selectedImages.map((img) => ({
          id: img.id,
          label: img.refs[0] ?? img.shortId,
          refs: img.refs,
        }))}
        onClose={() => setDeleteDialogOpen(false)}
        onSuccess={handleDeleteSuccess}
        open={deleteDialogOpen}
      />
    </div>
  );
};
export default ImageList;
