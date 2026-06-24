import { Icon } from "@iconify/react";
import React, {
  useCallback,
  useEffect,
  useEffectEvent,
  useRef,
  useState,
} from "react";

import { linuxio } from "@/api";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppCollapse from "@/components/ui/AppCollapse";
import AppTypography from "@/components/ui/AppTypography";
import { joinPath as joinPathUtil } from "@/utils/path";

import "./directory-tree.css";

// ============================================================================
// Types
// ============================================================================

interface DirectoryTreeProps {
  fileFilter?: (node: TreeNodeData) => boolean;
  includeFiles?: boolean;
  onBrowsePathChange?: (path: string) => void;
  onSelect: (path: string) => void;
  rootPath?: string;
  selectableTypes?: TreeNodeKind[];
  selectedPath?: string;
}

type TreeNodeKind = "directory" | "file";

interface TreeNodeData {
  children?: TreeNodeData[];
  kind: TreeNodeKind;
  loaded: boolean;
  name: string;
  path: string;
}

// ============================================================================
// TreeNode (single row)
// ============================================================================

const TreeNode: React.FC<{
  node: TreeNodeData;
  depth: number;
  isSelectable: (node: TreeNodeData) => boolean;
  onBrowsePathChange?: (path: string) => void;
  selectedPath?: string;
  onSelect: (path: string) => void;
  onToggle: (node: TreeNodeData) => Promise<void>;
}> = ({
  node,
  depth,
  isSelectable,
  onBrowsePathChange,
  selectedPath,
  onSelect,
  onToggle,
}) => {
  const [expanded, setExpanded] = useState(false);
  const [loading, setLoading] = useState(false);
  const isSelected = selectedPath === node.path;
  const directory = node.kind === "directory";
  const selectable = isSelectable(node);

  const toggleDirectory = async () => {
    if (!directory) {
      return;
    }
    if (!expanded) {
      setLoading(true);
      await onToggle(node);
      setLoading(false);
      setExpanded(true);
    } else {
      setExpanded(false);
    }
  };

  const handleClick = () => {
    if (selectable) {
      onSelect(node.path);
      return;
    }
    if (directory) {
      onBrowsePathChange?.(node.path);
    }
    void toggleDirectory();
  };

  const handleToggleClick = (event: React.MouseEvent<HTMLButtonElement>) => {
    event.stopPropagation();
    if (directory) {
      onBrowsePathChange?.(node.path);
    }
    void toggleDirectory();
  };

  return (
    <div>
      <div
        aria-expanded={expanded}
        aria-selected={isSelected}
        className={`directory-tree__node ${isSelected ? "directory-tree__node--selected" : ""} ${selectable ? "" : "directory-tree__node--toggle-only"}`}
        data-tree-name={node.name.toLowerCase()}
        data-tree-path={node.path}
        onClick={handleClick}
        role="treeitem"
        style={{ paddingLeft: depth * 20 + 8 }}
      >
        {directory ? (
          <button
            aria-label={`${expanded ? "Collapse" : "Expand"} ${node.name}`}
            className="directory-tree__toggle"
            disabled={loading}
            onClick={handleToggleClick}
            type="button"
          >
            {loading ? (
              <AppCircularProgress size={14} />
            ) : (
              <Icon
                icon={expanded ? "mdi:chevron-down" : "mdi:chevron-right"}
                width={16}
              />
            )}
          </button>
        ) : (
          <span className="directory-tree__chevron">
            <span className="directory-tree__chevron-spacer" />
          </span>
        )}
        <Icon
          className="directory-tree__icon"
          icon={
            directory
              ? expanded
                ? "mdi:folder-open"
                : "mdi:folder"
              : "mdi:file-outline"
          }
          width={18}
        />
        <AppTypography
          className="directory-tree__label"
          color="inherit"
          component="span"
          fontSize="inherit"
          noWrap
          title={node.path}
          variant="body2"
        >
          {node.name}
        </AppTypography>
      </div>
      <AppCollapse in={expanded} unmountOnExit>
        {node.children?.map((child) => (
          <TreeNode
            depth={depth + 1}
            isSelectable={isSelectable}
            key={child.path}
            node={child}
            onBrowsePathChange={onBrowsePathChange}
            onSelect={onSelect}
            onToggle={onToggle}
            selectedPath={selectedPath}
          />
        ))}
      </AppCollapse>
    </div>
  );
};

// ============================================================================
// DirectoryTree (root component)
// ============================================================================

function joinPath(parent: string, name: string): string {
  return `${joinPathUtil(parent, name)}/`.replace(/\/{2,}/g, "/");
}

function joinFilePath(parent: string, name: string): string {
  return joinPathUtil(parent, name).replace(/\/{2,}/g, "/");
}

const DirectoryTree: React.FC<DirectoryTreeProps> = ({
  fileFilter,
  includeFiles = false,
  onBrowsePathChange,
  rootPath = "/",
  selectableTypes = ["directory"],
  selectedPath,
  onSelect,
}) => {
  const containerRef = useRef<HTMLDivElement>(null);
  const [roots, setRoots] = useState<TreeNodeData[]>(() => [
    { name: rootPath, path: rootPath, kind: "directory", loaded: false },
  ]);

  const isSelectable = useCallback(
    (node: TreeNodeData) => selectableTypes.includes(node.kind),
    [selectableTypes],
  );

  const loadChildren = useCallback(
    async (node: TreeNodeData) => {
      if (node.loaded || node.kind !== "directory") return;

      try {
        const resource = await linuxio.filebrowser.resource_get({
          path: node.path,
        });

        const children = resourceChildren(resource, node.path, {
          fileFilter,
          includeFiles,
        });

        setRoots((prev) => updateNode(prev, node.path, children));
      } catch {
        setRoots((prev) => updateNode(prev, node.path, []));
      }
    },
    [fileFilter, includeFiles],
  );

  // Keyboard: press a letter to jump to the first visible folder starting with it
  const handleTreeKeyDown = useEffectEvent((e: KeyboardEvent) => {
    if (e.key.length !== 1 || e.ctrlKey || e.metaKey || e.altKey) return;

    const letter = e.key.toLowerCase();
    const el = containerRef.current;
    if (!el) return;

    const nodes = el.querySelectorAll<HTMLElement>("[data-tree-name]");
    for (const node of nodes) {
      if (node.dataset.treeName?.startsWith(letter)) {
        const path = node.dataset.treePath;
        if (path) onSelect(path);
        node.scrollIntoView({ block: "nearest" });
        break;
      }
    }
  });

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;

    el.addEventListener("keydown", handleTreeKeyDown);
    return () => el.removeEventListener("keydown", handleTreeKeyDown);
  }, []);

  return (
    <div className="directory-tree" ref={containerRef} role="tree" tabIndex={0}>
      {roots.map((node) => (
        <TreeNode
          depth={0}
          isSelectable={isSelectable}
          key={node.path}
          node={node}
          onBrowsePathChange={onBrowsePathChange}
          onSelect={onSelect}
          onToggle={loadChildren}
          selectedPath={selectedPath}
        />
      ))}
    </div>
  );
};

// ============================================================================
// Helpers
// ============================================================================

// resource_get returns an ExtendedFileInfo: directory children arrive pre-split
// into `folders` and `files`, neither of which carries its own `path`.
interface ResourceChild {
  name: string;
}

function resourceChildren(
  resource: unknown,
  parentPath: string,
  {
    fileFilter,
    includeFiles,
  }: {
    fileFilter?: (node: TreeNodeData) => boolean;
    includeFiles: boolean;
  },
): TreeNodeData[] {
  const raw = resource as {
    files?: ResourceChild[];
    folders?: ResourceChild[];
  };
  const folders = raw.folders ?? [];
  const files = includeFiles ? (raw.files ?? []) : [];

  const dirs: TreeNodeData[] = folders.map((folder) => ({
    kind: "directory",
    loaded: false,
    name: folder.name,
    path: joinPath(parentPath, folder.name),
  }));

  const fileNodes: TreeNodeData[] = files
    .map((file) => ({
      kind: "file" as const,
      loaded: true,
      name: file.name,
      path: joinFilePath(parentPath, file.name),
    }))
    .filter((node) => fileFilter?.(node) ?? true);

  return [
    ...dirs.sort((a, b) => a.name.localeCompare(b.name)),
    ...fileNodes.sort((a, b) => a.name.localeCompare(b.name)),
  ];
}

function updateNode(
  nodes: TreeNodeData[],
  targetPath: string,
  children: TreeNodeData[],
): TreeNodeData[] {
  return nodes.map((n) => {
    if (n.path === targetPath) {
      return { ...n, children, loaded: true };
    }
    if (n.children) {
      return { ...n, children: updateNode(n.children, targetPath, children) };
    }
    return n;
  });
}

export default DirectoryTree;
