import { Icon } from "@iconify/react";
import { memo, useCallback, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";

import AppActionIconButton from "@/components/ui/AppActionIconButton";
import AppHeaderSearch from "@/components/ui/AppHeaderSearch";
import AppIconButton from "@/components/ui/AppIconButton";
import AppMobileActionsMenu from "@/components/ui/AppMobileActionsMenu";
import AppPopover from "@/components/ui/AppPopover";
import HeaderActions from "@/components/ui/HeaderActions";
import ViewModeToggle from "@/components/ui/ViewModeToggle";
import { useHeaderActionSlot } from "@/contexts/HeaderActionSlotContext";
import { useBackgroundTaskActions } from "@/hooks/backgroundTasks/useBackgroundTaskActions";
import { useIsIndexing } from "@/hooks/backgroundTasks/useIsIndexing";
import { iconSize } from "@/theme/constants";
import type { ViewMode } from "@/types/filebrowser";

interface FileBrowserHeaderActionsProps {
  isMobile: boolean;
  onSearchChange: (value: string) => void;
  onSwitchView: () => void;
  onToggleHiddenFiles: () => void;
  searchQuery: string;
  showHiddenFiles: boolean;
  viewMode: ViewMode;
}

const FileBrowserHeaderActions = memo(function FileBrowserHeaderActions({
  isMobile,
  onSearchChange,
  onSwitchView,
  onToggleHiddenFiles,
  searchQuery,
  showHiddenFiles,
  viewMode,
}: FileBrowserHeaderActionsProps) {
  const headerActionSlot = useHeaderActionSlot();
  const [actionsAnchorEl, setActionsAnchorEl] = useState<HTMLElement | null>(
    null,
  );
  const [mobileSearchAnchorEl, setMobileSearchAnchorEl] =
    useState<HTMLElement | null>(null);
  const mobileSearchRef = useRef<HTMLDivElement | null>(null);
  const { startIndexer, openIndexerDialog } = useBackgroundTaskActions();
  const isIndexing = useIsIndexing();

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
      <AppActionIconButton
        ariaLabel="Index filesystem"
        disabled={isIndexing}
        icon="mdi:sync"
        iconSize={20}
        label={isIndexing ? "Indexing..." : "Index filesystem"}
        loading={isIndexing}
        onClick={handleIndexer}
      />
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
          <AppHeaderSearch
            onChange={onSearchChange}
            placeholder="Search files and folders..."
            value={searchQuery}
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
