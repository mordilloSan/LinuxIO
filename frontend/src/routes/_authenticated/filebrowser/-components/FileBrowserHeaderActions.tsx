import { Icon } from "@iconify/react";
import { useNavigate } from "@tanstack/react-router";
import {
  memo,
  useCallback,
  useLayoutEffect,
  useRef,
  useState,
  type DragEvent,
} from "react";
import { createPortal } from "react-dom";

import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppIconButton from "@/components/ui/AppIconButton";
import AppMobileActionsMenu from "@/components/ui/AppMobileActionsMenu";
import AppPopover from "@/components/ui/AppPopover";
import HeaderActions from "@/components/ui/HeaderActions";
import ViewModeToggle from "@/components/ui/ViewModeToggle";
import useAuth from "@/hooks/useAuth";
import { useHeaderActionSlot } from "@/hooks/useHeaderActionSlot";
import { useBackgroundTaskActions } from "@/tasks/useBackgroundTaskActions";
import { useIsIndexing } from "@/tasks/useIsIndexing";
import { iconSize } from "@/theme/constants";

import { hasInternalDrag, readInternalDrag } from "../-internalDrag";
import type { ViewMode } from "../-types";
import FileBrowserSearch from "./FileBrowserSearch";

interface FileBrowserHeaderActionsProps {
  isMobile: boolean;
  onSearchCaseSensitiveChange: (value: boolean) => void;
  onSearchChange: (value: string) => void;
  onSwitchView: () => void;
  onToggleHiddenFiles: () => void;
  /** Items dragged onto the Trash button move to the Trash. */
  onTrashItems?: (paths: string[]) => Promise<unknown>;
  searchQuery: string;
  searchCaseSensitive: boolean;
  showHiddenFiles: boolean;
  viewMode: ViewMode;
}

const FileBrowserHeaderActions = memo(function FileBrowserHeaderActions({
  isMobile,
  onSearchCaseSensitiveChange,
  onSearchChange,
  onSwitchView,
  onToggleHiddenFiles,
  onTrashItems,
  searchQuery,
  searchCaseSensitive,
  showHiddenFiles,
  viewMode,
}: FileBrowserHeaderActionsProps) {
  const headerActionSlot = useHeaderActionSlot();
  const navigate = useNavigate();
  const [actionsAnchorEl, setActionsAnchorEl] = useState<HTMLElement | null>(
    null,
  );
  const [mobileSearchAnchorEl, setMobileSearchAnchorEl] =
    useState<HTMLElement | null>(null);
  const [trashDropTarget, setTrashDropTarget] = useState(false);
  const mobileSearchRef = useRef<HTMLDivElement | null>(null);
  const { startIndexer, openIndexerDialog } = useBackgroundTaskActions();
  const isIndexing = useIsIndexing();
  // Indexing and search run through privileged routes.
  const { privileged } = useAuth();

  const handleIndexer = useCallback(() => {
    setActionsAnchorEl(null);
    openIndexerDialog();
    void startIndexer({});
  }, [openIndexerDialog, startIndexer]);
  const handleActionsTriggerRef = useCallback(
    (element: HTMLButtonElement | null) => {
      if (!element) {
        setActionsAnchorEl(null);
        setMobileSearchAnchorEl(null);
      }
    },
    [],
  );
  useLayoutEffect(() => {
    if (!mobileSearchAnchorEl) return;

    mobileSearchRef.current
      ?.querySelector<HTMLInputElement | HTMLTextAreaElement>("input, textarea")
      ?.focus();
  }, [mobileSearchAnchorEl]);
  const closeActionsMenu = useCallback(() => setActionsAnchorEl(null), []);
  const handleMobileSearchClose = useCallback(() => {
    const focusedElement = document.activeElement;
    const trigger = mobileSearchAnchorEl;

    setMobileSearchAnchorEl(null);
    if (trigger && mobileSearchRef.current?.contains(focusedElement)) {
      trigger.focus();
    }
  }, [mobileSearchAnchorEl]);

  const optionActions = (onDone?: () => void) => (
    <>
      <AppActionIconButton
        icon={showHiddenFiles ? "mdi:eye" : "mdi:eye-off"}
        iconSize={20}
        label={showHiddenFiles ? "Hide hidden files" : "Show hidden files"}
        onClick={() => {
          onDone?.();
          onToggleHiddenFiles();
        }}
      />
      {privileged ? (
        <AppActionIconButton
          ariaLabel="Index filesystem"
          disabled={isIndexing}
          icon="mdi:sync"
          iconSize={20}
          label={isIndexing ? "Indexing..." : "Index filesystem"}
          loading={isIndexing}
          onClick={handleIndexer}
        />
      ) : null}
      {/* The Trash is a place: the button opens it in this folder's stead,
          and Back returns. Items dragged onto it move to the Trash. */}
      <span
        onDragLeave={(event: DragEvent) => {
          if (
            event.currentTarget.contains(event.relatedTarget as Node | null)
          ) {
            return;
          }
          setTrashDropTarget(false);
        }}
        onDragOver={(event: DragEvent) => {
          if (!onTrashItems || !hasInternalDrag(event.dataTransfer)) return;
          event.preventDefault();
          event.dataTransfer.dropEffect = "move";
          setTrashDropTarget(true);
        }}
        onDrop={(event: DragEvent) => {
          if (!onTrashItems || !hasInternalDrag(event.dataTransfer)) return;
          // The page's upload drop handler must never see an internal drag.
          event.preventDefault();
          event.stopPropagation();
          setTrashDropTarget(false);
          const paths = readInternalDrag(event.dataTransfer);
          if (paths.length === 0) return;
          onTrashItems(paths).catch(() => {
            // The move to Trash reports its own failure.
          });
        }}
        style={{ display: "inline-flex" }}
      >
        <AppActionIconButton
          ariaLabel="Trash"
          color={
            trashDropTarget ? "var(--app-palette-primary-main)" : undefined
          }
          icon="mdi:trash-can-outline"
          iconSize={20}
          label="Trash"
          onClick={() => {
            onDone?.();
            void navigate({ search: { trash: true }, to: "." });
          }}
        />
      </span>
    </>
  );
  const viewAction = (onDone?: () => void) => (
    <ViewModeToggle
      alternateMode="list"
      onViewModeChange={() => {
        onDone?.();
        onSwitchView();
      }}
      viewMode={viewMode}
    />
  );

  if (!isMobile) {
    return <HeaderActions options={optionActions()} view={viewAction()} />;
  }

  const headerActionHost = headerActionSlot?.host ?? null;
  const mobileActions = (
    <>
      <AppIconButton
        aria-expanded={Boolean(actionsAnchorEl)}
        aria-label="Actions"
        color={searchQuery ? "primary" : "secondary"}
        onClick={(event) => {
          setMobileSearchAnchorEl(null);
          setActionsAnchorEl(event.currentTarget);
        }}
        ref={handleActionsTriggerRef}
        size={headerActionHost ? "medium" : "small"}
      >
        <Icon
          height={headerActionHost ? iconSize.md : 20}
          icon="mdi:tune"
          width={headerActionHost ? iconSize.md : 20}
        />
      </AppIconButton>
      <AppMobileActionsMenu
        anchorEl={actionsAnchorEl}
        onClose={closeActionsMenu}
        open={Boolean(actionsAnchorEl)}
      >
        {privileged ? (
          <AppIconButton
            aria-label="Search"
            color={searchQuery ? "primary" : "default"}
            onClick={() => {
              setMobileSearchAnchorEl(actionsAnchorEl);
              setActionsAnchorEl(null);
            }}
            size="small"
          >
            <Icon height={20} icon="mdi:magnify" width={20} />
          </AppIconButton>
        ) : null}
        <HeaderActions
          options={optionActions(closeActionsMenu)}
          view={viewAction(closeActionsMenu)}
        />
      </AppMobileActionsMenu>
      <AppPopover
        anchorEl={mobileSearchAnchorEl}
        anchorOrigin={{ vertical: "bottom", horizontal: "right" }}
        keepMounted
        onClose={handleMobileSearchClose}
        open={Boolean(mobileSearchAnchorEl)}
        paperStyle={{
          width: "min(400px, calc(100vw - 16px))",
          padding: 8,
        }}
        transformOrigin={{ vertical: "top", horizontal: "right" }}
      >
        <div
          className="file-browser-header__mobile-search"
          ref={mobileSearchRef}
          role="search"
          style={{ width: "100%" }}
        >
          <FileBrowserSearch
            caseSensitive={searchCaseSensitive}
            onCaseSensitiveChange={onSearchCaseSensitiveChange}
            onSearchChange={onSearchChange}
            searchQuery={searchQuery}
          />
        </div>
      </AppPopover>
    </>
  );

  return headerActionHost
    ? createPortal(mobileActions, headerActionHost)
    : mobileActions;
});

export default FileBrowserHeaderActions;
