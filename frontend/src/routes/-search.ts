export type SearchInput = Record<string, unknown>;

const CANONICAL_OPERATION_ID =
  /^(?!00000000-0000-0000-0000-000000000000$)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

export function optionalHandoffOperationId(search: SearchInput): {
  handoffOperationId?: string;
} {
  const value = search.handoffOperationId;
  return typeof value === "string" && CANONICAL_OPERATION_ID.test(value)
    ? { handoffOperationId: value }
    : {};
}

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
