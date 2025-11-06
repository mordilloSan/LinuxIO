import { useMediaQuery, useTheme } from "@mui/material";
import { styled } from "@mui/material/styles";
import React, { useMemo } from "react";

export type BreadcrumbItem = {
  label: string;
  path: string;
  isLast: boolean;
};

type FilebrowserBreadcrumbsProps = {
  path: string;
  onNavigate: (path: string) => void;
  showGallerySize?: boolean;
  gallerySize?: number;
  onGallerySizeChange?: (next: number) => void;
};

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
    accumulated.push(encodeURIComponent(segment));
    const isLast = index === segments.length - 1;
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
    <BreadcrumbContainer id="breadcrumbs">
      <BreadcrumbList>
        <BreadcrumbListItem>
          <BreadcrumbButton
            type="button"
            onClick={handleHome}
            aria-label="Go to root"
            title="Home"
          >
            <span className="material-icons">home</span>
          </BreadcrumbButton>
        </BreadcrumbListItem>
        {breadcrumbs.map((crumb, index) => (
          <BreadcrumbListItem key={`${crumb.path}-${crumb.label}-${index}`}>
            <BreadcrumbButton
              type="button"
              onClick={() => onNavigate(crumb.path)}
              aria-label={`breadcrumb-link-${crumb.label}`}
              title={crumb.label}
              className={crumb.isLast ? "active" : undefined}
            >
              {crumb.label}
            </BreadcrumbButton>
          </BreadcrumbListItem>
        ))}
      </BreadcrumbList>
      {showGallerySize && onGallerySizeChange && (
        <GallerySize className="gallery-size card">
          Size<span className="sr-only">:</span>
          <RangeInput
            type="range"
            id="gallery-size"
            name="gallery-size"
            min={1}
            max={8}
            value={gallerySize}
            onChange={(event) =>
              onGallerySizeChange(Number.parseInt(event.target.value, 10))
            }
          />
        </GallerySize>
      )}
    </BreadcrumbContainer>
  );
};

export default FilebrowserBreadcrumbs;

const BreadcrumbContainer = styled("div")(({ theme }) => ({
  margin: "0.5em 0",
  overflowY: "hidden",
  display: "flex",
  alignItems: "center",
  gap: "1rem",
  flexWrap: "nowrap",
  [theme.breakpoints.down("sm")]: {
    gap: "0.75rem",
  },
}));

const BreadcrumbButton = styled("button")(({ theme }) => {
  const isDark = theme.palette.mode === "dark";
  const breadcrumbBackground = isDark
    ? "#283136"
    : "#d0d4d8";
  const breadcrumbHover = theme.palette.primary.main;
  const breadcrumbForeground = isDark
    ? theme.palette.text.primary
    : "#5a5a5a";
  const backgroundBehind = theme.palette.background.default;

  return {
    display: "flex",
    alignItems: "center",
    justifyContent: "center",
    height: "3.25em",
    background: breadcrumbBackground,
    textAlign: "center",
    padding: "0 1em",
    paddingLeft: "2em",
    position: "relative",
    textDecoration: "none",
    color: breadcrumbForeground,
    border: "none",
    borderRadius: 0,
    cursor: "pointer",
    font: "inherit",
    fontSize: "0.875rem",
    lineHeight: 1,
    "&::after": {
      content: '""',
      borderTop: "1.625em solid transparent",
      borderBottom: "1.625em solid transparent",
      borderLeft: `1em solid ${breadcrumbBackground}`,
      position: "absolute",
      right: "-1em",
      top: 0,
      bottom: 0,
      zIndex: 1,
    },
    "&::before": {
      content: '""',
      width: 0,
      height: 0,
      borderTop: "1.625em solid transparent",
      borderBottom: "1.625em solid transparent",
      borderLeft: `1.2em solid ${backgroundBehind}`,
      position: "absolute",
      left: 0,
      top: 0,
      bottom: 0,
    },
    "& span.material-icons": {
      fontSize: "1.5rem",
      lineHeight: 1,
      display: "flex",
      alignItems: "center",
      justifyContent: "center",
    },
    "&:hover": {
      background: breadcrumbHover,
      "&::after": {
        borderLeftColor: breadcrumbHover,
      },
    },
    "&.active": {
      "&::after": {
        display: "none",
      },
    },
  };
});

const BreadcrumbList = styled("ul")(({ theme }) => ({
  display: "flex",
  alignItems: "center",
  margin: 0,
  padding: 0,
  listStyle: "none",
  maxWidth: "100%",
  [theme.breakpoints.down("sm")]: {
    maxWidth: "calc(100% - 8rem)",
    overflow: "hidden",
  },
}));

const BreadcrumbListItem = styled("li")({
  display: "inline-block",
  margin: "0 8px 0 0",
  "&:last-of-type": {
    marginRight: 0,
  },
  "&:first-of-type": {
    marginLeft: 0,
  },
  "&:first-of-type button": {
    borderTopLeftRadius: "1em",
    borderBottomLeftRadius: "1em",
    paddingLeft: "1.5em",
  },
  "&:first-of-type button::before": {
    display: "none",
  },
  "&:last-of-type button": {
    paddingRight: "1.5em",
    borderTopRightRadius: "1em",
    borderBottomRightRadius: "1em",
  },
  "&:last-of-type button::after": {
    display: "none",
  },
});

const GallerySize = styled("div")(({ theme }) => ({
  display: "flex",
  alignItems: "center",
  gap: "0.75em",
  flex: "0 0 auto",
  padding: "0.35em 0.75em",
  maxWidth: "8.5em",
  borderRadius: "0.75em",
  background:
    theme.palette.mode === "dark"
      ? "rgba(37, 49, 55, 0.33)"
      : "rgba(37, 49, 55, 0.08)",
}));

const RangeInput = styled("input")(({ theme }) => ({
  margin: 0,
  width: "100%",
  maxWidth: "6.25em",
  accentColor: theme.palette.primary.main,
}));
