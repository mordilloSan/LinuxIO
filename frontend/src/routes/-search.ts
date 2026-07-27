export type SearchInput = Record<string, unknown>;

export function optionalString<TKey extends string>(
  search: SearchInput,
  key: TKey,
): { [P in TKey]?: string } {
  const value = search[key];
  return typeof value === "string" && value
    ? ({ [key]: value } as { [P in TKey]: string })
    : {};
}

export function optionalNumber<TKey extends string>(
  search: SearchInput,
  key: TKey,
): { [P in TKey]?: number } {
  const value = search[key];
  return typeof value === "number"
    ? ({ [key]: value } as { [P in TKey]: number })
    : {};
}

export function optionalBoolean<TKey extends string>(
  search: SearchInput,
  key: TKey,
): { [P in TKey]?: boolean } {
  const value = search[key];
  return typeof value === "boolean"
    ? ({ [key]: value } as { [P in TKey]: boolean })
    : {};
}
