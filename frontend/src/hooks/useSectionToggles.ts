import { useConfigValue } from "@/hooks/useConfig";

type CollapsedSections = Record<string, string[]>;

/**
 * A page's collapsible sections. The page declares it next to its sections,
 * `as const`, so the `sections` tuple is the union every read and toggle is
 * typed against. Only collapsed names are stored, under `key` in the
 * `collapsedSections` preference, and the backend never reads them: a section
 * added to the tuple starts open for everyone.
 */
export interface SectionPage<S extends string> {
  readonly key: string;
  readonly sections: readonly S[];
}

/** Reads the page's sections from `collapsedSections`: open unless stored. */
export function createIsSectionOpen<S extends string>(
  collapsed: CollapsedSections,
  page: SectionPage<S>,
): (section: S) => boolean {
  const names = collapsed[page.key] ?? [];
  return (section) => !names.includes(section);
}

/**
 * Flips one section. The page's stored list is rebuilt from its own tuple, so
 * names it no longer renders are dropped, and a page with nothing collapsed
 * is removed rather than stored empty.
 */
export function toggleSection<S extends string>(
  collapsed: CollapsedSections,
  page: SectionPage<S>,
  section: S,
): CollapsedSections {
  const current = collapsed[page.key] ?? [];
  const names = page.sections.filter(
    (name) => current.includes(name) !== (name === section),
  );
  const next = { ...collapsed };
  if (names.length > 0) next[page.key] = names;
  else delete next[page.key];
  return next;
}

export function useSectionToggles<S extends string>(page: SectionPage<S>) {
  const [collapsed, setCollapsed] = useConfigValue("collapsedSections");
  return {
    isOpen: createIsSectionOpen(collapsed, page),
    toggle: (section: S) =>
      setCollapsed((prev) => toggleSection(prev, page, section)),
  };
}
