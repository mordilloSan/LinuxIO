import { Icon } from "@iconify/react";
import { useQueries } from "@tanstack/react-query";
import {
  startTransition,
  useId,
  useRef,
  useState,
  type FocusEvent,
  type KeyboardEvent,
  type MouseEvent,
} from "react";

import { linuxio } from "@/api";
import AppCircularProgress from "@/components/ui/AppCircularProgress";
import AppCollapse from "@/components/ui/AppCollapse";
import AppTypography from "@/components/ui/AppTypography";
import {
  ensureTrailingSlash,
  joinPath as joinPathUtil,
  stripTrailingSlash,
} from "@/utils/path";

import "./directory-tree.css";

// ============================================================================
// Types
// ============================================================================

interface DirectoryTreeProps {
  "aria-label"?: string;
  /**
   * Move focus into the tree when it mounts: to the root at once, then to the
   * row of `selectedPath` when its folders have loaded.
   */
  autoFocus?: boolean;
  fileFilter?: (node: TreeNodeData) => boolean;
  id?: string;
  includeFiles?: boolean;
  onBrowsePathChange?: (path: string) => void;
  onSelect: (path: string) => void;
  rootPath?: string;
  selectableTypes?: TreeNodeKind[];
  selectedPath?: string;
}

type TreeNodeKind = "directory" | "file";

interface TreeNodeData {
  kind: TreeNodeKind;
  name: string;
  /** A folder path ends in "/", a file path does not. */
  path: string;
}

/**
 * What every row reads from the tree it belongs to. Nothing in it changes
 * while the tree is being walked or folders are opened, so those never draw
 * rows that did not change.
 */
interface TreeState {
  fileFilter?: (node: TreeNodeData) => boolean;
  includeFiles: boolean;
  /** Ref callback of every row; it gives a row the focus it is owed on mount. */
  onAttachRow: (row: HTMLDivElement | null) => void;
  onBrowsePathChange?: (path: string) => void;
  onSelect: (path: string) => void;
  seededPaths: string[];
  selectableTypes: TreeNodeKind[];
  selectedPath?: string;
}

type Listing = { files?: string[] | null; folders?: string[] | null };

interface TreeNodeProps {
  depth: number;
  node: TreeNodeData;
  /** 1-based place among the rows of the same folder. */
  position: number;
  setSize: number;
  tree: TreeState;
}

const ROW_SELECTOR = '[role="treeitem"]';
const TOGGLE_SELECTOR = ".directory-tree__toggle";
const DEFAULT_SELECTABLE: TreeNodeKind[] = ["directory"];
const TYPEAHEAD_RESET_MS = 500;

// ============================================================================
// Rows
// ============================================================================

const TreeRow = ({
  depth,
  error,
  errorId,
  expanded,
  loading = false,
  node,
  onToggle,
  position,
  setSize,
  tree,
}: TreeNodeProps & {
  /** Why the folder could not be listed, shown after its name. */
  error?: string;
  /** The id the row's description points at while `error` is shown. */
  errorId?: string;
  /** Set on folders only; a file cannot be expanded. */
  expanded?: boolean;
  loading?: boolean;
  onToggle?: () => void;
}) => {
  const { onAttachRow } = tree;
  const directory = node.kind === "directory";
  const selected = samePath(tree.selectedPath, node.path);
  const selectable = tree.selectableTypes.includes(node.kind);

  const handleToggleClick = (event: MouseEvent<HTMLButtonElement>) => {
    event.stopPropagation();
    // Focus stays on the row, so the arrow keys carry on from it.
    event.currentTarget.closest<HTMLElement>(ROW_SELECTOR)?.focus();
    onToggle?.();
  };

  // A row that can be chosen is chosen; a folder that cannot is opened.
  const handleClick = () => {
    if (selectable) {
      tree.onSelect(node.path);
    } else {
      onToggle?.();
    }
  };

  return (
    <div
      // The name stays the folder's, which type-ahead and assistive technology
      // look for; the reason it cannot be listed is its description.
      aria-describedby={error ? errorId : undefined}
      aria-expanded={expanded}
      aria-label={node.name}
      aria-level={depth + 1}
      aria-posinset={position}
      aria-selected={selected}
      aria-setsize={setSize}
      className={`directory-tree__node ${selected ? "directory-tree__node--selected" : ""} ${selectable ? "" : "directory-tree__node--toggle-only"}`}
      data-tree-name={node.name.toLowerCase()}
      data-tree-path={node.path}
      onClick={handleClick}
      ref={onAttachRow}
      role="treeitem"
      style={{ paddingLeft: depth * 20 + 8 }}
      // The tab stop starts on the root; `moveTabStop` moves it from then on.
      tabIndex={depth === 0 ? 0 : -1}
    >
      {directory ? (
        <button
          aria-label={`${expanded ? "Collapse" : "Expand"} ${node.name}`}
          className="directory-tree__toggle"
          disabled={loading}
          onClick={handleToggleClick}
          tabIndex={-1}
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
      {error ? (
        <span className="directory-tree__error" id={errorId} title={error}>
          {error}
        </span>
      ) : null}
    </div>
  );
};

const DirectoryNode = ({
  depth,
  node,
  position,
  setSize,
  tree,
}: TreeNodeProps) => {
  const seeded = tree.seededPaths.includes(node.path);
  // Undefined until the folder is first opened. It lives here and not in the
  // tree, so opening one folder draws that folder and no other row.
  const [expanded, setExpanded] = useState(seeded ? true : undefined);
  // A folder gets its listing query when it is first opened and keeps it, so a
  // folder that is closing still has rows to slide away. `useQueries` with no
  // query does no work; a `useQuery` here would give every folder of a long
  // listing an observer and a cache entry before anyone opened it.
  const queries =
    expanded === undefined
      ? []
      : [
          {
            ...childrenQuery(node.path, tree.includeFiles),
            enabled: expanded,
            // The row shows why a folder cannot be listed. A toast would repeat
            // it, and the folders leading to the current value would raise one
            // just by the picker opening.
            meta: { silent: true },
          },
        ];
  const query = useQueries({ queries }).at(0);
  const listing = query?.data;
  // Shown for as long as the query is in error, also while an older listing
  // is still on screen after a refresh failed.
  const error = query?.isError ? query.error.message : undefined;
  const errorId = useId();
  // A folder whose listing failed stays closed; activating it again retries.
  const open = expanded === true && listing !== undefined;
  // AppCollapse slides open when `in` turns true after it has mounted. A folder
  // opened for the current value mounts it with the listing instead, so its
  // rows are in place at once for the focus that follows.
  const awaitsListing = seeded && !listing;

  const toggle = () => {
    tree.onBrowsePathChange?.(node.path);

    if (!expanded) {
      // A long folder is thousands of rows. As a transition they render
      // without blocking the page; as an urgent update the click would hold
      // it until every row and icon is in place, about twice as long.
      startTransition(() => setExpanded(true));
    } else if (listing) {
      setExpanded(false);
    } else if (query?.isError) {
      void query.refetch();
    }
    // Otherwise the listing is still loading and the activation is ignored.
  };

  return (
    <div>
      <TreeRow
        depth={depth}
        error={error}
        errorId={errorId}
        expanded={open}
        loading={expanded === true && query?.isPending === true}
        node={node}
        onToggle={toggle}
        position={position}
        setSize={setSize}
        tree={tree}
      />
      {awaitsListing ? null : (
        <AppCollapse in={open} unmountOnExit>
          <div data-tree-closed={open ? undefined : ""}>
            {listing ? (
              <FolderRows
                depth={depth + 1}
                listing={listing}
                parentPath={node.path}
                tree={tree}
              />
            ) : null}
          </div>
        </AppCollapse>
      )}
    </div>
  );
};

/**
 * The rows of one folder. A component of its own so they are drawn again only
 * when the listing changes: the query above reports more than that (a refresh
 * starting and ending, the folder closing), and without this boundary each
 * report would draw every row again.
 */
const FolderRows = ({
  depth,
  listing,
  parentPath,
  tree,
}: {
  depth: number;
  listing: Listing;
  parentPath: string;
  tree: TreeState;
}) => {
  const children = childNodes(listing, parentPath, tree);

  // The fragment is what makes the React Compiler treat this as a component
  // and cache the rows: it skips a function that returns a bare array.
  return (
    <>
      {children.map((child, index) =>
        child.kind === "directory" ? (
          <DirectoryNode
            depth={depth}
            key={child.path}
            node={child}
            position={index + 1}
            setSize={children.length}
            tree={tree}
          />
        ) : (
          <TreeRow
            depth={depth}
            key={child.path}
            node={child}
            position={index + 1}
            setSize={children.length}
            tree={tree}
          />
        ),
      )}
    </>
  );
};

// ============================================================================
// Tree
// ============================================================================

const Tree = ({
  "aria-label": ariaLabel,
  autoFocus = false,
  fileFilter,
  id,
  includeFiles = false,
  onBrowsePathChange,
  onSelect,
  rootPath,
  selectableTypes = DEFAULT_SELECTABLE,
  selectedPath,
}: DirectoryTreeProps & { rootPath: string }) => {
  const [seededPaths] = useState(() => pathsToValue(rootPath, selectedPath));
  // The focus ring follows the keys used inside the tree, because the app-wide
  // ring only appears after Tab.
  const [keyboard, setKeyboard] = useState(false);
  // Rows to focus as they mount. Using the tree cancels what is left, so a
  // slow listing never pulls focus away from the user.
  const focusOnMount = useRef(
    autoFocus ? [rootPath, ...(selectedPath ? [selectedPath] : [])] : [],
  );
  const typeahead = useRef({ at: 0, text: "" });

  const attachRow = (row: HTMLDivElement | null) => {
    const path = row?.dataset.treePath;
    if (!row || path === undefined) return;

    const wanted = focusOnMount.current;
    if (!wanted.some((candidate) => samePath(candidate, path))) return;

    focusOnMount.current = wanted.filter(
      (candidate) => !samePath(candidate, path),
    );
    // The root mounts before the popover is placed and must not scroll the
    // page; a row that loads later scrolls into the tree's view.
    row.focus({ preventScroll: path === rootPath });
  };

  /** Focus the next row whose name starts with what was just typed. */
  const typeAhead = (
    event: KeyboardEvent<HTMLDivElement>,
    rows: HTMLElement[],
    index: number,
  ) => {
    const letter = event.key.toLowerCase();
    const previous = typeahead.current;
    const text =
      event.timeStamp - previous.at > TYPEAHEAD_RESET_MS
        ? letter
        : previous.text + letter;
    typeahead.current = { at: event.timeStamp, text };

    // One letter, typed once or repeated, steps through the rows that start
    // with it. Different letters narrow to the row that starts with them all.
    const stepping = text === letter.repeat(text.length);
    const prefix = stepping ? letter : text;
    const from = Math.max(index, 0) + (stepping ? 1 : 0);

    for (let offset = 0; offset < rows.length; offset += 1) {
      const candidate = rows[(from + offset) % rows.length];
      if (candidate.dataset.treeName?.startsWith(prefix)) {
        candidate.focus();
        return;
      }
    }
  };

  /** Returns false for a key the tree leaves alone, such as Tab or Escape. */
  const handleNavigationKey = (
    key: string,
    {
      index,
      row,
      rows,
    }: { index: number; row: HTMLElement; rows: HTMLElement[] },
  ): boolean => {
    const isOpen = row.getAttribute("aria-expanded") === "true";
    // The keys press the controls a pointer would, so the keyboard can never
    // do what a click cannot: Enter clicks the row, Right and Left its toggle.
    const toggle = () =>
      row.querySelector<HTMLElement>(TOGGLE_SELECTOR)?.click();

    switch (key) {
      case "ArrowDown":
        rows[index + 1]?.focus();
        break;
      case "ArrowUp":
        rows[index - 1]?.focus();
        break;
      case "Home":
        rows[0]?.focus();
        break;
      case "End":
        rows.at(-1)?.focus();
        break;
      case "ArrowRight":
        if (!isOpen) {
          toggle();
        } else if (rowLevel(rows[index + 1]) > rowLevel(row)) {
          rows[index + 1].focus();
        }
        break;
      case "ArrowLeft":
        if (isOpen) {
          toggle();
        } else {
          rows
            .slice(0, Math.max(index, 0))
            .findLast((other) => rowLevel(other) < rowLevel(row))
            ?.focus();
        }
        break;
      case "Enter":
      case " ":
        row.click();
        break;
      default:
        return false;
    }

    // Any of these keys ends the run of letters being typed.
    typeahead.current = { at: 0, text: "" };
    return true;
  };

  const handleKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    focusOnMount.current = [];
    if (event.ctrlKey || event.metaKey || event.altKey) return;

    const row =
      event.target instanceof HTMLElement
        ? event.target.closest<HTMLElement>(ROW_SELECTOR)
        : null;
    if (!row) return;

    const rows = visibleRows(event.currentTarget);
    const index = rows.indexOf(row);

    if (event.key.length === 1 && event.key !== " ") {
      typeAhead(event, rows, index);
    } else if (!handleNavigationKey(event.key, { index, row, rows })) {
      return;
    }

    // Handled here: keep it from the page behind (form submit, scrolling,
    // single-key shortcuts).
    event.preventDefault();
    event.stopPropagation();
    setKeyboard(true);
  };

  const handlePointerDown = () => {
    focusOnMount.current = [];
    setKeyboard(false);
  };

  // One row is in the tab order, and it follows focus. It is moved on the
  // rows themselves and not kept in state: state every row reads would draw
  // the whole tree again on each arrow key, which a folder of a few thousand
  // entries cannot afford. React leaves the attribute alone because the value
  // it renders for a row never changes.
  const moveTabStop = (event: FocusEvent<HTMLDivElement>) => {
    const row = event.target.closest<HTMLElement>(ROW_SELECTOR);
    if (!row || row.tabIndex === 0) return;

    const stops = event.currentTarget.querySelectorAll<HTMLElement>(
      `${ROW_SELECTOR}[tabindex="0"]`,
    );
    for (const stop of stops) stop.tabIndex = -1;
    row.tabIndex = 0;
  };

  const tree: TreeState = {
    fileFilter,
    includeFiles,
    onAttachRow: attachRow,
    onBrowsePathChange,
    onSelect,
    seededPaths,
    selectableTypes,
    selectedPath,
  };

  return (
    <div
      aria-label={ariaLabel}
      className="directory-tree"
      data-keyboard={keyboard ? "" : undefined}
      id={id}
      onFocus={moveTabStop}
      onKeyDown={handleKeyDown}
      onPointerDown={handlePointerDown}
      role="tree"
    >
      <DirectoryNode
        depth={0}
        node={{ kind: "directory", name: rootPath, path: rootPath }}
        position={1}
        setSize={1}
        tree={tree}
      />
    </div>
  );
};

/**
 * A folder tree that loads one level at a time.
 *
 * It opens with the root expanded and, when `selectedPath` lies under the
 * root, with the folders leading to it expanded as well. Every listing is a
 * query of its own, so an open folder follows invalidation like any other
 * view of the file system.
 *
 * Keyboard: Up/Down/Home/End move between rows, Right opens a folder or steps
 * into it, Left closes it or steps out, Enter and Space do what a click does,
 * and typing letters moves to the next row that starts with them.
 */
const DirectoryTree = ({ rootPath = "/", ...props }: DirectoryTreeProps) => {
  const root = ensureTrailingSlash(rootPath);

  // Keyed on the root, so a root that changes while the tree is open starts a
  // fresh tree instead of keeping rows of the old one.
  return <Tree key={root} {...props} rootPath={root} />;
};

// ============================================================================
// Helpers
// ============================================================================

function childrenQuery(path: string, includeFiles: boolean) {
  return linuxio.filebrowser.directory_children({
    path: stripTrailingSlash(path),
    includeFiles,
  });
}

/** True when both name the same entry, with or without a trailing slash. */
function samePath(a: string | undefined, b: string): boolean {
  return !!a && stripTrailingSlash(a) === stripTrailingSlash(b);
}

/**
 * The folders to expand so the row of `selectedPath` is on screen: the root
 * and each folder between it and the value. The value itself stays closed.
 * A folder that does not exist is never listed by its parent, so it is never
 * requested.
 */
function pathsToValue(rootPath: string, selectedPath?: string): string[] {
  const paths = [rootPath];
  const target = stripTrailingSlash(selectedPath ?? "");
  if (!`${target}/`.startsWith(rootPath)) return paths;

  const between = target.slice(rootPath.length).split("/").filter(Boolean);
  let current = rootPath;
  for (const name of between.slice(0, -1)) {
    current = joinPath(current, name);
    paths.push(current);
  }
  return paths;
}

/** The rows a user can reach now, leaving out those of a folder that is closing. */
function visibleRows(tree: HTMLElement): HTMLElement[] {
  const rows = Array.from(tree.querySelectorAll<HTMLElement>(ROW_SELECTOR));
  // Checking every row costs milliseconds in a long folder, so it is done only
  // while a folder is in fact closing.
  return tree.querySelector("[data-tree-closed]")
    ? rows.filter((row) => !row.closest("[data-tree-closed]"))
    : rows;
}

function rowLevel(row: HTMLElement | undefined): number {
  return Number(row?.getAttribute("aria-level") ?? 0);
}

function joinPath(parent: string, name: string): string {
  return `${joinPathUtil(parent, name)}/`.replace(/\/{2,}/g, "/");
}

function joinFilePath(parent: string, name: string): string {
  return joinPathUtil(parent, name).replace(/\/{2,}/g, "/");
}

function childNodes(
  listing: Listing,
  parentPath: string,
  { fileFilter, includeFiles }: Pick<TreeState, "fileFilter" | "includeFiles">,
): TreeNodeData[] {
  const folders = listing.folders ?? [];
  const files = includeFiles ? (listing.files ?? []) : [];

  const dirs: TreeNodeData[] = folders.map((name) => ({
    kind: "directory",
    name,
    path: joinPath(parentPath, name),
  }));

  const fileNodes: TreeNodeData[] = files
    .map((name) => ({
      kind: "file" as const,
      name,
      path: joinFilePath(parentPath, name),
    }))
    .filter((node) => fileFilter?.(node) ?? true);

  return [
    ...dirs.sort((a, b) => a.name.localeCompare(b.name)),
    ...fileNodes.sort((a, b) => a.name.localeCompare(b.name)),
  ];
}

export default DirectoryTree;
