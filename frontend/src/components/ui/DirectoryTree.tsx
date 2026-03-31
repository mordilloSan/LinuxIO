import { Icon } from "@iconify/react";
import React, { useState, useCallback, useEffect, useEffectEvent, useRef } from "react";

import { linuxio } from "@/api";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppCollapse from "@/components/ui/AppCollapse";

import "./directory-tree.css";

// ============================================================================
// Types
// ============================================================================

interface DirectoryTreeProps {
  rootPath?: string;
  selectedPath?: string;
  onSelect: (path: string) => void;
}

interface TreeNodeData {
  name: string;
  path: string;
  children?: TreeNodeData[];
  loaded: boolean;
}

// ============================================================================
// TreeNode (single row)
// ============================================================================

const TreeNode: React.FC<{
  node: TreeNodeData;
  depth: number;
  selectedPath?: string;
  onSelect: (path: string) => void;
  onToggle: (node: TreeNodeData) => Promise<void>;
}> = ({ node, depth, selectedPath, onSelect, onToggle }) => {
  const [expanded, setExpanded] = useState(false);
  const [loading, setLoading] = useState(false);
  const isSelected = selectedPath === node.path;

  const handleClick = async () => {
    onSelect(node.path);
    if (!expanded) {
      setLoading(true);
      await onToggle(node);
      setLoading(false);
      setExpanded(true);
    } else {
      setExpanded(false);
    }
  };

  return (
    <div>
      <div
        className={`directory-tree__node ${isSelected ? "directory-tree__node--selected" : ""}`}
        style={{ paddingLeft: depth * 20 + 8 }}
        onClick={handleClick}
        role="treeitem"
        aria-expanded={expanded}
        aria-selected={isSelected}
        data-tree-name={node.name.toLowerCase()}
        data-tree-path={node.path}
      >
        <span className="directory-tree__chevron">
          {loading ? (
            <AppCircularProgress size={14} />
          ) : (
            <Icon
              icon={expanded ? "mdi:chevron-down" : "mdi:chevron-right"}
              width={16}
            />
          )}
        </span>
        <Icon
          icon={expanded ? "mdi:folder-open" : "mdi:folder"}
          width={18}
          className="directory-tree__icon"
        />
        <span className="directory-tree__label">{node.name}</span>
      </div>
      <AppCollapse in={expanded} unmountOnExit>
        {node.children?.map((child) => (
          <TreeNode
            key={child.path}
            node={child}
            depth={depth + 1}
            selectedPath={selectedPath}
            onSelect={onSelect}
            onToggle={onToggle}
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
  const base = parent.endsWith("/") ? parent : parent + "/";
  return (base + name + "/").replace(/\/{2,}/g, "/");
}

const DirectoryTree: React.FC<DirectoryTreeProps> = ({
  rootPath = "/",
  selectedPath,
  onSelect,
}) => {
  const containerRef = useRef<HTMLDivElement>(null);
  const [roots, setRoots] = useState<TreeNodeData[]>(() => [
    { name: rootPath, path: rootPath, loaded: false },
  ]);

  const loadChildren = useCallback(async (node: TreeNodeData) => {
    if (node.loaded) return;

    try {
      const resource = await linuxio.filebrowser.resource_get.call(node.path);

      const raw = resource as unknown as {
        folders?: { name: string }[];
      };
      const folders = raw.folders ?? [];

      const dirs: TreeNodeData[] = folders
        .map((f) => ({
          name: f.name,
          path: joinPath(node.path, f.name),
          loaded: false,
        }))
        .sort((a, b) => a.name.localeCompare(b.name));

      setRoots((prev) => updateNode(prev, node.path, dirs));
    } catch {
      setRoots((prev) => updateNode(prev, node.path, []));
    }
  }, []);

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
    <div ref={containerRef} className="directory-tree" role="tree" tabIndex={0}>
      {roots.map((node) => (
        <TreeNode
          key={node.path}
          node={node}
          depth={0}
          selectedPath={selectedPath}
          onSelect={onSelect}
          onToggle={loadChildren}
        />
      ))}
    </div>
  );
};

// ============================================================================
// Helpers
// ============================================================================

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
