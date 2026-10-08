import type * as acp from "@agentclientprotocol/sdk";
import { Icon } from "@iconify/react";
import { type CSSProperties, useState } from "react";

import AppIconButton from "@/components/ui/AppIconButton";
import AppMenu, { AppMenuItem } from "@/components/ui/AppMenu";

export type SelectOption = Extract<acp.SessionConfigOption, { type: "select" }>;

/** Grouped or flat agent options, as one list with optional group headings. */
function flatten(options: acp.SessionConfigSelectOptions) {
  if (options.length > 0 && "group" in options[0]) {
    return (options as acp.SessionConfigSelectGroup[]).flatMap((group) => [
      { heading: group.name, key: `group:${group.group}` },
      ...group.options.map((option) => ({ option, key: option.value })),
    ]);
  }
  return (options as acp.SessionConfigSelectOption[]).map((option) => ({
    option,
    key: option.value,
  }));
}

const currentName = (select: SelectOption | undefined) => {
  if (!select) return null;
  for (const entry of flatten(select.options)) {
    if ("option" in entry && entry.option.value === select.currentValue) {
      return entry.option.name;
    }
  }
  return select.currentValue;
};

const NAV_KEYS = new Set([
  "ArrowUp",
  "ArrowDown",
  "ArrowLeft",
  "ArrowRight",
  "Home",
  "End",
  "PageUp",
  "PageDown",
]);

/**
 * Effort as a stepped slider pinned under the model list, like the Claude app:
 * "Effort (High)" with a dot for each level. Dragging only moves the thumb;
 * the level is sent when the drag or key press ends, so a sweep across the
 * track is one request rather than one per level.
 */
function EffortSlider({
  effort,
  onChange,
}: {
  effort: SelectOption;
  onChange: (configId: string, value: string) => void;
}) {
  const levels = flatten(effort.options).flatMap((entry) =>
    "option" in entry ? [entry.option] : [],
  );
  const current = Math.max(
    0,
    levels.findIndex((level) => level.value === effort.currentValue),
  );
  // The thumb's position while dragging; the menu unmounts on close, which
  // drops it.
  const [draft, setDraft] = useState<number | null>(null);
  const index = draft ?? current;
  const commit = () => {
    const level = draft === null ? undefined : levels[draft];
    if (level && level.value !== effort.currentValue) {
      onChange(effort.id, level.value);
    }
  };

  if (levels.length === 0) return null;
  return (
    <div className="assistant__effort" role="presentation">
      <span className="assistant__effort-label">
        {effort.name}{" "}
        <span className="assistant__effort-value">({levels[index].name})</span>
      </span>
      <span
        className="assistant__effort-slider"
        // Where the accent line ends: from the first level to the thumb.
        style={
          {
            "--effort-fill": `${levels.length > 1 ? (index / (levels.length - 1)) * 100 : 0}%`,
          } as CSSProperties
        }
      >
        <span aria-hidden="true" className="assistant__effort-dots">
          {levels.map((level, position) => (
            <span
              className={
                position <= index
                  ? "assistant__effort-dot assistant__effort-dot--on"
                  : "assistant__effort-dot"
              }
              key={level.value}
            />
          ))}
        </span>
        <input
          aria-label={effort.name}
          aria-valuetext={levels[index].name}
          className="assistant__effort-range"
          max={levels.length - 1}
          min={0}
          onBlur={commit}
          onChange={(event) => setDraft(Number(event.target.value))}
          // The menu moves focus on arrow keys; the slider needs them.
          onKeyDown={(event) => {
            if (NAV_KEYS.has(event.key)) event.stopPropagation();
          }}
          onKeyUp={commit}
          onPointerUp={commit}
          step={1}
          type="range"
          value={index}
        />
      </span>
    </div>
  );
}

/**
 * One chip for model and effort ("Opus 5.5 High"). It opens a menu as wide as
 * the prompt: every model with its description, then the effort slider, which
 * stays visible while the model list scrolls. Picking a model closes the menu;
 * moving the slider leaves it open so both can be set in one visit.
 */
export default function ModelMenu({
  model,
  effort,
  disabled,
  widthAnchor,
  onChange,
}: {
  model?: SelectOption;
  effort?: SelectOption;
  disabled: boolean;
  /** The prompt box: the menu aligns to it and takes its width. */
  widthAnchor: HTMLElement | null;
  onChange: (configId: string, value: string) => void;
}) {
  const [open, setOpen] = useState<{
    anchor: HTMLElement;
    width: number;
  } | null>(null);
  const modelName = currentName(model);
  const effortName = currentName(effort);
  const label = [modelName, effortName].filter(Boolean).join(" ");

  return (
    <>
      <AppIconButton
        aria-haspopup="menu"
        aria-label={`Model: ${label}`}
        className="assistant__pill assistant__model-chip"
        color="secondary"
        disabled={disabled}
        onClick={(event) => {
          const anchor = widthAnchor ?? event.currentTarget;
          setOpen({ anchor, width: anchor.getBoundingClientRect().width });
        }}
      >
        {modelName ? (
          <span className="assistant__pill-major">{modelName}</span>
        ) : null}
        {effortName ? (
          <span className="assistant__pill-minor">{effortName}</span>
        ) : null}
      </AppIconButton>
      <AppMenu
        anchorEl={open?.anchor ?? null}
        anchorOrigin={{ vertical: "top", horizontal: "left" }}
        ariaLabel="Model"
        className="assistant__model-menu"
        minWidth={open?.width}
        onClose={() => setOpen(null)}
        open={open !== null}
        style={open ? { width: open.width } : undefined}
        transformOrigin={{ vertical: "bottom", horizontal: "left" }}
      >
        {model ? (
          <>
            <div className="assistant__menu-header" role="presentation">
              {model.name}
            </div>
            {flatten(model.options).map((entry) =>
              "heading" in entry ? (
                <div
                  className="assistant__menu-subheader"
                  key={entry.key}
                  role="presentation"
                >
                  {entry.heading}
                </div>
              ) : (
                <AppMenuItem
                  endAdornment={
                    entry.option.value === model.currentValue ? (
                      <Icon icon="mdi:check" />
                    ) : null
                  }
                  key={entry.key}
                  onClick={() => {
                    setOpen(null);
                    onChange(model.id, entry.option.value);
                  }}
                  selected={entry.option.value === model.currentValue}
                >
                  <span className="assistant__mode-name">
                    {entry.option.name}
                  </span>
                  {entry.option.description ? (
                    <span className="assistant__mode-description">
                      {entry.option.description}
                    </span>
                  ) : null}
                </AppMenuItem>
              ),
            )}
          </>
        ) : null}
        {effort ? <EffortSlider effort={effort} onChange={onChange} /> : null}
      </AppMenu>
    </>
  );
}
