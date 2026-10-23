import { Icon } from "@iconify/react";
import { memo } from "react";

import "./FileIcon.css";

import { getIconColor, getIconForType } from "./fileIconUtils";

interface FileIconProps {
  /** Applied to the icon wrapper, so a parent can animate the icon. */
  className?: string;
  filename?: string;
  hidden?: boolean;
  isDirectory: boolean;
  isSymlink?: boolean;
  size?: number;
}

const FileIcon = memo(
  ({
    className,
    isDirectory,
    filename,
    hidden,
    size = 70,
    isSymlink = false,
  }: FileIconProps) => {
    const iconName = isDirectory ? "mdi:folder" : getIconForType(filename);
    const iconColor = isDirectory
      ? "var(--app-palette-primary-main)"
      : getIconColor(filename);
    const wrapperOpacity = hidden ? 0.25 : 1;

    const wrapperClass = [
      "file-icon",
      !isDirectory && iconColor === undefined && "file-icon--default-color",
      className,
    ]
      .filter(Boolean)
      .join(" ");

    if (!isSymlink) {
      return (
        <span className={wrapperClass} style={{ opacity: wrapperOpacity }}>
          <Icon
            color={iconColor}
            height={size}
            icon={iconName}
            style={{ flexShrink: 0 }}
            width={size}
          />
        </span>
      );
    }

    // Render with symlink overlay
    return (
      <div
        className={`${wrapperClass} file-icon--symlink`}
        style={{ opacity: wrapperOpacity }}
      >
        <Icon
          color={iconColor}
          height={size}
          icon={iconName}
          style={{ flexShrink: 0 }}
          width={size}
        />
        <Icon
          height={size * 0.35}
          icon="mdi:link"
          style={{
            position: "absolute",
            color: "var(--app-palette-text-secondary)",
            bottom: isDirectory ? "20%" : "10%",
            right: isDirectory ? "10%" : "15%",
            transform: "rotate(-45deg)",
            filter:
              "drop-shadow(0px 1px 2px color-mix(in srgb, black, transparent 50%))",
          }}
          width={size * 0.35}
        />
      </div>
    );
  },
);

FileIcon.displayName = "FileIcon";

export default FileIcon;
