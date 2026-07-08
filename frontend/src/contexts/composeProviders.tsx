import {
  createElement,
  type ComponentType,
  type Key,
  type PropsWithChildren,
  type ReactNode,
} from "react";

type ProviderComponent = ComponentType<PropsWithChildren>;

type ProviderProps = Record<string, unknown> & { key?: Key };

export type ProviderEntry =
  | ProviderComponent
  | readonly [ProviderComponent, ProviderProps];

interface ComposeProvidersProps extends PropsWithChildren {
  providers: readonly ProviderEntry[];
}

/**
 * Type-safe way to build a props-carrying entry: `props` is checked against the
 * provider's own prop type (children excluded — the composer supplies it).
 * `key` is allowed so one entry can remount its subtree without remounting the
 * providers above it.
 */
export function withProps<P extends { children?: ReactNode }>(
  provider: ComponentType<P>,
  props: Omit<P, "children"> & { key?: Key },
): ProviderEntry {
  return [provider as ProviderComponent, props as ProviderProps];
}

/**
 * Renders a provider chain from a flat list instead of a nested pyramid.
 * Entries render first-to-last as outermost-to-innermost. Use `withProps` to
 * pass props (including `key`) to an entry.
 */
export function ComposeProviders({
  providers,
  children,
}: ComposeProvidersProps) {
  return providers.reduceRight<ReactNode>((subtree, entry) => {
    const [provider, props] = Array.isArray(entry)
      ? entry
      : ([entry, undefined] as const);
    return createElement(
      provider as ComponentType<ProviderProps>,
      props,
      subtree,
    );
  }, children);
}
