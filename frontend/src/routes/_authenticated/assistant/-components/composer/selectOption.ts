import type * as acp from "@agentclientprotocol/sdk/unstable";

export type SelectOption = Extract<acp.SessionConfigOption, { type: "select" }>;

/** Grouped or flat agent options, as one list with optional group headings. */
export function flatten(options: acp.SessionConfigSelectOptions) {
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

/**
 * The agent lists "Default" as an entry of its own; its description names the
 * model it stands for. Drop it and point at that entry instead, so the list
 * and the slider only show real choices.
 */
export function withoutDefault(select: SelectOption): SelectOption {
  const all = flatten(select.options).flatMap((entry) =>
    "option" in entry ? [entry.option] : [],
  );
  const placeholder = all.find((option) => option.value === "default");
  if (!placeholder) return select;
  const real = (option: acp.SessionConfigSelectOption) =>
    option.value !== "default";
  const options =
    "group" in select.options[0]
      ? (select.options as acp.SessionConfigSelectGroup[]).map((group) => ({
          ...group,
          options: group.options.filter(real),
        }))
      : (select.options as acp.SessionConfigSelectOption[]).filter(real);
  // Effort's "Default" has no description; the agent recommends medium.
  const resolved =
    all.find((option) => option.name === placeholder.description) ??
    all.find((option) => option.value === "medium") ??
    all.find(real);
  return {
    ...select,
    options,
    currentValue:
      select.currentValue === "default" && resolved
        ? resolved.value
        : select.currentValue,
  };
}

/** The name of the value an option is set to. */
export const currentName = (select: SelectOption | undefined) => {
  if (!select) return null;
  for (const entry of flatten(select.options)) {
    if ("option" in entry && entry.option.value === select.currentValue) {
      return entry.option.name;
    }
  }
  return select.currentValue;
};
