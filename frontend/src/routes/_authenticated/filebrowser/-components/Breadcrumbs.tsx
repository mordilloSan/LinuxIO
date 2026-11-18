import { Icon } from "@iconify/react";
import { useState, type CSSProperties, type DragEvent } from "react";

import AppButton from "@/components/ui/AppButton";
import { useMediaQuery } from "@/hooks/useMediaQuery";
import { HomeFilledIcon } from "@/icons/svg";
import { down } from "@/theme/breakpoints";
import { isDirectoryPath } from "@/utils/path";

import {
  hasInternalDrag,
  hasTrashDrag,
  acceptInternalDrop,
  acceptTrashDrop,
  refuseFileDrop,
  type TrashDragItem,
} from "../-internalDrag";

interface BreadcrumbItem {
  isLast: boolean;
  label: string;
  path: string;
}

/** A place that is not a folder, shown as the last segment of the trail. */
export interface BreadcrumbPlace {
  icon: string;
  label: string;
  /** Items dragged from a folder onto the place. */
  onDropItems?: (paths: string[]) => void;
}

interface FilebrowserBreadcrumbsProps {
  /** Items dropped on a trail segment move (or, with Ctrl, copy) into it. */
  onDropItems?: (
    paths: string[],
    destinationDir: string,
    copy: boolean,
  ) => void;
  onNavigate: (path: string) => void;
  /** Items dragged out of the Trash onto a segment are restored into it. */
  onRestoreItems?: (items: TrashDragItem[], destinationDir: string) => void;
  path: string;
  /** Shown after the folders of `path`, which then lead back to them. */
  place?: BreadcrumbPlace;
}

// The drop target key of the place segment; no folder path looks like it.
const PLACE_TARGET = "\0place";

// Static CSS styles (injected once)
// Uses MUI CSS variables and light-dark() function for theme-aware colors
const breadcrumbStyles = `
  .linuxio-breadcrumb-container {
    margin: 0.5em 0;
    overflow-y: hidden;
    display: flex;
    align-items: center;
    gap: 1rem;
    flex-wrap: nowrap;
    /* Stretch to the header column. Shrink-to-fit would make the list's
       percentage max-width resolve against the trail's own width, so any
       reservation subtracted from it clips the trail no matter how short. */
    flex: 1 1 auto;
    min-width: 0;
  }

  @media (max-width: 599.95px) {
    .linuxio-breadcrumb-container {
      gap: 0.75rem;
    }
  }

  .linuxio-breadcrumb-button {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 3.25em;
    background: var(--linuxio-filebrowser-breadcrumb-bg);
    text-align: center;
    padding: 0 1em;
    padding-left: 2em;
    position: relative;
    text-decoration: none;
    color: var(--linuxio-filebrowser-breadcrumb-text);
    border: none;
    border-radius: 0;
    min-width: 0;
    cursor: pointer;
    font: inherit;
    font-size: 0.875rem;
    line-height: 1;
  }

  .linuxio-breadcrumb-button::after {
    content: "";
    border-top: 1.625em solid transparent;
    border-bottom: 1.625em solid transparent;
    border-left: 1em solid var(--linuxio-filebrowser-breadcrumb-bg);
    position: absolute;
    right: -1em;
    top: 0;
    bottom: 0;
    z-index: 1;
  }

  .linuxio-breadcrumb-button::before {
    content: "";
    width: 0;
    height: 0;
    border-top: 1.625em solid transparent;
    border-bottom: 1.625em solid transparent;
    border-left: 1.2em solid var(--app-palette-background-default);
    position: absolute;
    left: 0;
    top: 0;
    bottom: 0;
  }

  .linuxio-breadcrumb-home-icon {
    width: 1.5rem;
    height: 1.5rem;
    line-height: 1;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .linuxio-breadcrumb-button:hover {
    background: var(--app-palette-primary-main);
  }

  .linuxio-breadcrumb-button:hover::after {
    border-left-color: var(--app-palette-primary-main);
  }

  .linuxio-breadcrumb-button.drop-target {
    background: var(--app-palette-primary-main);
  }

  .linuxio-breadcrumb-button.drop-target::after {
    border-left-color: var(--app-palette-primary-main);
  }

  .linuxio-breadcrumb-button.active::after {
    display: none;
  }

  .linuxio-breadcrumb-list {
    display: flex;
    align-items: center;
    margin: 0;
    padding: 0;
    list-style: none;
    max-width: 100%;
  }

  @media (max-width: 599.95px) {
    .linuxio-breadcrumb-list {
      overflow: hidden;
    }
  }

  .linuxio-breadcrumb-list-item {
    display: inline-block;
    margin: 0 8px 0 0;
  }

  .linuxio-breadcrumb-list-item:last-of-type {
    margin-right: 0;
  }

  .linuxio-breadcrumb-list-item:first-of-type {
    margin-left: 0;
  }

  .linuxio-breadcrumb-list-item:first-of-type .linuxio-breadcrumb-button {
    border-top-left-radius: 1em;
    border-bottom-left-radius: 1em;
    padding-left: 1.5em;
  }

  .linuxio-breadcrumb-list-item:first-of-type .linuxio-breadcrumb-button::before {
    display: none;
  }

  .linuxio-breadcrumb-list-item:last-of-type .linuxio-breadcrumb-button {
    padding-right: 1.5em;
    border-top-right-radius: 1em;
    border-bottom-right-radius: 1em;
  }

  .linuxio-breadcrumb-list-item:last-of-type .linuxio-breadcrumb-button::after {
    display: none;
  }
`;

// Inject styles once
if (
  typeof document !== "undefined" &&
  !document.getElementById("breadcrumbStyles")
) {
  const style = document.createElement("style");
  style.id = "breadcrumbStyles";
  style.textContent = breadcrumbStyles;
  document.head.appendChild(style);
}

const normalizePath = (input: string): string => {
  if (!input) return "/";
  if (input === "/") return "/";
  const trimmed = input.trim();
  if (!trimmed) return "/";
  return trimmed.startsWith("/") ? trimmed : `/${trimmed}`;
};

const splitSegments = (path: string): string[] => {
  const cleaned = path.replace(/^\/+|\/+$/g, "");
  if (!cleaned) {
    return [];
  }
  return cleaned.split("/").filter(Boolean);
};

const buildBreadcrumbs = (
  normalizedPath: string,
  maxSegments: number,
): BreadcrumbItem[] => {
  const segments = splitSegments(normalizedPath);
  const breadcrumbs: BreadcrumbItem[] = [];

  const accumulated: string[] = [];

  segments.forEach((segment, index) => {
    accumulated.push(segment);
    const isLast = index === segments.length - 1;
    // Pass unencoded filesystem paths - encoding is handled by onNavigate
    breadcrumbs.push({
      label: segment,
      path: `/${accumulated.join("/")}${isLast && isDirectoryPath(normalizedPath) ? "/" : ""}`,
      isLast,
    });
  });

  if (breadcrumbs.length > maxSegments) {
    const trimmed = breadcrumbs.slice(-maxSegments);
    return [
      { label: "...", path: trimmed[0].path, isLast: false },
      ...trimmed.map((b, idx) => ({
        ...b,
        isLast: idx === trimmed.length - 1,
      })),
    ];
  }

  return breadcrumbs;
};

const FilebrowserBreadcrumbs = ({
  path,
  onNavigate,
  onDropItems,
  onRestoreItems,
  place,
}: FilebrowserBreadcrumbsProps) => {
  const isMobile = useMediaQuery(down("sm"));
  const normalizedPath = normalizePath(path);

  // A place takes the room of one folder segment; on a phone it leaves only
  // Home before it.
  const maxSegments = (isMobile ? 1 : 3) - (place ? 1 : 0);
  const trail =
    maxSegments > 0 ? buildBreadcrumbs(normalizedPath, maxSegments) : [];
  const breadcrumbs = place
    ? trail.map((crumb) => ({ ...crumb, isLast: false }))
    : trail;

  const [dropTargetPath, setDropTargetPath] = useState<string | null>(null);

  const leaveProps = (target: string) => ({
    onDragLeave: (event: DragEvent) => {
      if (event.currentTarget.contains(event.relatedTarget as Node | null)) {
        return;
      }
      setDropTargetPath((current) => (current === target ? null : current));
    },
  });

  // Drop handlers for one segment; segments are ancestors of what is shown, so
  // no dragged item can be the segment itself. Items dragged out of the
  // Trash are restored into the segment rather than moved.
  const dropProps = (path: string) => {
    if (!onDropItems && !onRestoreItems) return undefined;
    const restores = (event: DragEvent) =>
      Boolean(onRestoreItems) && hasTrashDrag(event.dataTransfer);
    const moves = (event: DragEvent) =>
      Boolean(onDropItems) && hasInternalDrag(event.dataTransfer);
    return {
      ...leaveProps(path),
      onDragOver: (event: DragEvent) => {
        if (!restores(event) && !moves(event)) {
          refuseFileDrop(event);
          return;
        }
        event.preventDefault();
        event.dataTransfer.dropEffect =
          moves(event) && event.ctrlKey ? "copy" : "move";
        setDropTargetPath(path);
      },
      onDrop: (event: DragEvent) => {
        if (!restores(event) && !moves(event)) {
          refuseFileDrop(event);
          return;
        }
        // The page's upload drop handler must never see an internal drag.
        event.preventDefault();
        event.stopPropagation();
        setDropTargetPath(null);
        if (restores(event)) {
          const items = acceptTrashDrop(event.dataTransfer);
          if (items.length > 0) onRestoreItems?.(items, path);
          return;
        }
        const paths = acceptInternalDrop(event.dataTransfer);
        if (paths.length > 0) onDropItems?.(paths, path, event.ctrlKey);
      },
    };
  };

  const onDropOnPlace = place?.onDropItems;
  const placeDropProps = onDropOnPlace && {
    ...leaveProps(PLACE_TARGET),
    onDragOver: (event: DragEvent) => {
      if (!hasInternalDrag(event.dataTransfer)) {
        refuseFileDrop(event);
        return;
      }
      event.preventDefault();
      event.dataTransfer.dropEffect = "move";
      setDropTargetPath(PLACE_TARGET);
    },
    onDrop: (event: DragEvent) => {
      if (!hasInternalDrag(event.dataTransfer)) {
        refuseFileDrop(event);
        return;
      }
      event.preventDefault();
      event.stopPropagation();
      setDropTargetPath(null);
      const paths = acceptInternalDrop(event.dataTransfer);
      if (paths.length > 0) onDropOnPlace(paths);
    },
  };

  const handleHome = () => onNavigate("/");
  const cssVars = {
    "--linuxio-filebrowser-breadcrumb-bg":
      "var(--app-file-browser-breadcrumb-background)",
    "--linuxio-filebrowser-breadcrumb-text":
      "var(--app-file-browser-breadcrumb-text)",
  } as CSSProperties;

  return (
    <div
      className="linuxio-breadcrumb-container"
      id="breadcrumbs"
      style={cssVars}
    >
      <ul className="linuxio-breadcrumb-list">
        <li className="linuxio-breadcrumb-list-item">
          <AppButton
            aria-label="Go to root"
            className={`linuxio-breadcrumb-button${dropTargetPath === "/" ? " drop-target" : ""}`}
            onClick={handleHome}
            title="Home"
            {...dropProps("/")}
          >
            <span className="linuxio-breadcrumb-home-icon">
              <HomeFilledIcon />
            </span>
          </AppButton>
        </li>
        {breadcrumbs.map((crumb, index) => (
          <li
            className="linuxio-breadcrumb-list-item"
            key={`${crumb.path}-${crumb.label}-${index}`}
          >
            <AppButton
              aria-label={`breadcrumb-link-${crumb.label}`}
              className={`linuxio-breadcrumb-button${crumb.isLast ? " active" : ""}${dropTargetPath === crumb.path ? " drop-target" : ""}`}
              onClick={() => onNavigate(crumb.path)}
              title={crumb.label}
              // The "..." crumb stands for a run of hidden segments.
              {...(crumb.label !== "..." && dropProps(crumb.path))}
            >
              {crumb.label}
            </AppButton>
          </li>
        ))}
        {place && (
          <li className="linuxio-breadcrumb-list-item">
            <AppButton
              aria-current="page"
              aria-label={`breadcrumb-link-${place.label}`}
              className={`linuxio-breadcrumb-button active${dropTargetPath === PLACE_TARGET ? " drop-target" : ""}`}
              title={place.label}
              {...placeDropProps}
            >
              <Icon
                height={18}
                icon={place.icon}
                style={{ marginRight: 6 }}
                width={18}
              />
              {place.label}
            </AppButton>
          </li>
        )}
      </ul>
    </div>
  );
};

export default FilebrowserBreadcrumbs;
