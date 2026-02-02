import { useMediaQuery, useTheme } from "@mui/material";
import React, { useMemo } from "react";

export interface BreadcrumbItem {
  label: string;
  path: string;
  isLast: boolean;
};

interface FilebrowserBreadcrumbsProps {
  path: string;
  onNavigate: (path: string) => void;
  showGallerySize?: boolean;
  gallerySize?: number;
  onGallerySizeChange?: (next: number) => void;
};

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
  }

  @media (max-width: 600px) {
    .linuxio-breadcrumb-container {
      gap: 0.75rem;
    }
  }

  .linuxio-breadcrumb-button {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 3.25em;
    background: light-dark(#d0d4d8, #283136);
    text-align: center;
    padding: 0 1em;
    padding-left: 2em;
    position: relative;
    text-decoration: none;
    color: light-dark(#5a5a5a, var(--mui-palette-text-primary));
    border: none;
    border-radius: 0;
    cursor: pointer;
    font: inherit;
    font-size: 0.875rem;
    line-height: 1;
  }

  .linuxio-breadcrumb-button::after {
    content: "";
    border-top: 1.625em solid transparent;
    border-bottom: 1.625em solid transparent;
    border-left: 1em solid light-dark(#d0d4d8, #283136);
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
    border-left: 1.2em solid var(--mui-palette-background-default);
    position: absolute;
    left: 0;
    top: 0;
    bottom: 0;
  }

  .linuxio-breadcrumb-button span.material-icons {
    font-size: 1.5rem;
    line-height: 1;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .linuxio-breadcrumb-button:hover {
    background: var(--mui-palette-primary-main);
  }

  .linuxio-breadcrumb-button:hover::after {
    border-left-color: var(--mui-palette-primary-main);
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

  @media (max-width: 600px) {
    .linuxio-breadcrumb-list {
      max-width: calc(100% - 8rem);
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

  .linuxio-gallery-size {
    display: flex;
    align-items: center;
    gap: 0.75em;
    flex: 0 0 auto;
    padding: 0.35em 0.75em;
    max-width: 8.5em;
    border-radius: 0.75em;
    background: color-mix(in srgb, #253137, transparent 67%);
  }

  [data-mui-color-scheme="dark"] .linuxio-gallery-size {
    background: color-mix(in srgb, #253137, transparent 67%);
  }

  [data-mui-color-scheme="light"] .linuxio-gallery-size {
    background: color-mix(in srgb, #253137, transparent 92%);
  }

  .linuxio-range-input {
    margin: 0;
    width: 100%;
    max-width: 6.25em;
    accent-color: var(--mui-palette-primary-main);
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
      path: `/${accumulated.join("/")}${isLast && normalizedPath.endsWith("/") ? "/" : ""}`,
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

const FilebrowserBreadcrumbs: React.FC<FilebrowserBreadcrumbsProps> = ({
  path,
  onNavigate,
  showGallerySize = false,
  gallerySize = 4,
  onGallerySizeChange,
}) => {
  const theme = useTheme();
  const isMobile = useMediaQuery(theme.breakpoints.down("sm"));
  const normalizedPath = normalizePath(path);

  const breadcrumbs = useMemo(() => {
    const maxSegments = isMobile ? 1 : 3;
    return buildBreadcrumbs(normalizedPath, maxSegments);
  }, [isMobile, normalizedPath]);

  const handleHome = () => onNavigate("/");

  return (
    <div id="breadcrumbs" className="linuxio-breadcrumb-container">
      <ul className="linuxio-breadcrumb-list">
        <li className="linuxio-breadcrumb-list-item">
          <button
            type="button"
            className="linuxio-breadcrumb-button"
            onClick={handleHome}
            aria-label="Go to root"
            title="Home"
          >
            <span className="material-icons">home</span>
          </button>
        </li>
        {breadcrumbs.map((crumb, index) => (
          <li
            key={`${crumb.path}-${crumb.label}-${index}`}
            className="linuxio-breadcrumb-list-item"
          >
            <button
              type="button"
              className={`linuxio-breadcrumb-button${crumb.isLast ? " active" : ""}`}
              onClick={() => onNavigate(crumb.path)}
              aria-label={`breadcrumb-link-${crumb.label}`}
              title={crumb.label}
            >
              {crumb.label}
            </button>
          </li>
        ))}
      </ul>
      {showGallerySize && onGallerySizeChange && (
        <div className="linuxio-gallery-size gallery-size card">
          Size<span className="sr-only">:</span>
          <input
            type="range"
            className="linuxio-range-input"
            id="gallery-size"
            name="gallery-size"
            min={1}
            max={8}
            value={gallerySize}
            onChange={(event) =>
              onGallerySizeChange(Number.parseInt(event.target.value, 10))
            }
          />
        </div>
      )}
    </div>
  );
};

export default FilebrowserBreadcrumbs;
