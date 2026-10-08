import type * as acp from "@agentclientprotocol/sdk";

import AppSelect from "@/components/ui/AppSelect";
import AppSwitch from "@/components/ui/AppSwitch";

import ModelMenu, { type SelectOption } from "./ModelMenu";

function isGrouped(
  options: acp.SessionConfigSelectOptions,
): options is acp.SessionConfigSelectGroup[] {
  return options.length > 0 && "group" in options[0];
}

// AppSelect reads <option> children directly, so this is a function, not a component.
export function renderOptions(options: acp.SessionConfigSelectOptions) {
  if (isGrouped(options)) {
    return options.map((group) => (
      <optgroup key={group.group} label={group.name}>
        {group.options.map((option) => (
          <option key={option.value} value={option.value}>
            {option.name}
          </option>
        ))}
      </optgroup>
    ));
  }
  return options.map((option) => (
    <option key={option.value} value={option.value}>
      {option.name}
    </option>
  ));
}

// Model and effort lead; the rest keep the agent's order.
const LEADING = ["model", "thought_level"];
const rank = (option: acp.SessionConfigOption) => {
  const index = LEADING.indexOf(option.category ?? "");
  return index === -1 ? LEADING.length : index;
};

function renderControl(
  option: acp.SessionConfigOption,
  disabled: boolean,
  onChange: (configId: string, value: string | boolean) => void,
) {
  if (option.type === "boolean") {
    return (
      <div className="assistant__bar-switch" key={option.id}>
        <AppSwitch
          aria-label={option.name}
          checked={option.currentValue}
          disabled={disabled}
          onChange={(_, checked) => onChange(option.id, checked)}
          size="small"
        />
        <span>{option.name}</span>
      </div>
    );
  }
  return (
    <AppSelect
      aria-label={option.name}
      placement="top"
      disableUnderline
      disabled={disabled}
      key={option.id}
      onChange={(event) => onChange(option.id, event.target.value)}
      size="small"
      value={option.currentValue}
      variant="standard"
    >
      {renderOptions(option.options)}
    </AppSelect>
  );
}

const isSelect = (
  option: acp.SessionConfigOption | undefined,
): option is SelectOption => option?.type === "select";

/**
 * Model and effort share one chip and one menu, the way the Claude app shows
 * "Opus 5.5 High"; any other options the agent advertises follow it.
 */
export default function ConfigControls({
  options,
  disabled,
  widthAnchor,
  onChange,
}: {
  options: acp.SessionConfigOption[];
  disabled: boolean;
  /** The prompt box, so the model menu can match its width. */
  widthAnchor: HTMLElement | null;
  onChange: (configId: string, value: string | boolean) => void;
}) {
  const sorted = options.toSorted((a, b) => rank(a) - rank(b));
  const model = sorted.find((option) => option.category === "model");
  const effort = sorted.find((option) => option.category === "thought_level");
  const chipModel = isSelect(model) ? model : undefined;
  const chipEffort = isSelect(effort) ? effort : undefined;
  const hasChip = Boolean(chipModel || chipEffort);
  // Model settings (Fast mode) live in the chip's menu, next to the model
  // they affect; anything else stays in the toolbar.
  const extras = hasChip
    ? sorted.filter(
        (option) =>
          option.category === "model_config" &&
          option !== chipModel &&
          option !== chipEffort,
      )
    : [];
  const rest = sorted.filter(
    (option) =>
      option !== chipModel && option !== chipEffort && !extras.includes(option),
  );
  return (
    <>
      {chipModel || chipEffort ? (
        <ModelMenu
          disabled={disabled}
          effort={chipEffort}
          extras={extras}
          model={chipModel}
          onChange={onChange}
          widthAnchor={widthAnchor}
        />
      ) : null}
      {rest.map((option) => renderControl(option, disabled, onChange))}
    </>
  );
}
