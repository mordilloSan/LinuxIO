import { LanguageSupport, type Language } from "@codemirror/language";
import { classHighlighter, highlightTree } from "@lezer/highlight";

import { languageLoaders } from "@/components/filebrowser/languageLoaders";
import type { LanguageName } from "@/components/filebrowser/languageNames";

export interface HighlightSpan {
  text: string;
  className?: string;
}

const languages = new Map<LanguageName, Promise<Language | null>>();

function loadLanguage(name: LanguageName) {
  let loaded = languages.get(name);
  if (!loaded) {
    const load = languageLoaders[name];
    loaded = load
      ? load().then((ext) =>
          ext instanceof LanguageSupport ? ext.language : (ext as Language),
        )
      : Promise.resolve(null);
    // A failed import must be retried next time, not cached.
    loaded.catch(() => languages.delete(name));
    languages.set(name, loaded);
  }
  return loaded;
}

/** Splits code into spans carrying CodeMirror's `tok-*` classes. */
export async function highlightCode(
  code: string,
  lang: LanguageName,
): Promise<HighlightSpan[]> {
  const language = await loadLanguage(lang);
  if (!language) return [{ text: code }];
  const tree = language.parser.parse(code);
  const spans: HighlightSpan[] = [];
  let pos = 0;
  highlightTree(tree, classHighlighter, (from, to, className) => {
    if (from > pos) spans.push({ text: code.slice(pos, from) });
    spans.push({ text: code.slice(from, to), className });
    pos = to;
  });
  if (pos < code.length) spans.push({ text: code.slice(pos) });
  return spans;
}
