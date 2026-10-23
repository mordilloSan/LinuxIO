import { StreamLanguage, type StreamParser } from "@codemirror/language";
import type { Extension } from "@uiw/react-codemirror";

import type { LanguageName } from "./languageNames";

function legacy<M>(
  load: () => Promise<M>,
  pick: (mod: M) => StreamParser<unknown>,
) {
  return () => load().then((mod) => StreamLanguage.define(pick(mod)));
}

const clike = () => import("@codemirror/legacy-modes/mode/clike");
const cssModes = () => import("@codemirror/legacy-modes/mode/css");
const jsModes = () => import("@codemirror/legacy-modes/mode/javascript");
const xmlModes = () => import("@codemirror/legacy-modes/mode/xml");

export const languageLoaders: Partial<
  Record<LanguageName, () => Promise<Extension>>
> = {
  c: legacy(clike, (m) => m.c),
  cpp: legacy(clike, (m) => m.cpp),
  css: legacy(cssModes, (m) => m.css),
  diff: legacy(
    () => import("@codemirror/legacy-modes/mode/diff"),
    (m) => m.diff,
  ),
  dockerfile: legacy(
    () => import("@codemirror/legacy-modes/mode/dockerfile"),
    (m) => m.dockerFile,
  ),
  go: legacy(
    () => import("@codemirror/legacy-modes/mode/go"),
    (m) => m.go,
  ),
  html: legacy(xmlModes, (m) => m.html),
  ini: legacy(
    () => import("@codemirror/legacy-modes/mode/properties"),
    (m) => m.properties,
  ),
  java: legacy(clike, (m) => m.java),
  javascript: legacy(jsModes, (m) => m.javascript),
  json: () => import("@codemirror/lang-json").then(({ json }) => json()),
  lua: legacy(
    () => import("@codemirror/legacy-modes/mode/lua"),
    (m) => m.lua,
  ),
  perl: legacy(
    () => import("@codemirror/legacy-modes/mode/perl"),
    (m) => m.perl,
  ),
  python: legacy(
    () => import("@codemirror/legacy-modes/mode/python"),
    (m) => m.python,
  ),
  ruby: legacy(
    () => import("@codemirror/legacy-modes/mode/ruby"),
    (m) => m.ruby,
  ),
  rust: legacy(
    () => import("@codemirror/legacy-modes/mode/rust"),
    (m) => m.rust,
  ),
  scss: legacy(cssModes, (m) => m.sCSS),
  shell: legacy(
    () => import("@codemirror/legacy-modes/mode/shell"),
    (m) => m.shell,
  ),
  sql: legacy(
    () => import("@codemirror/legacy-modes/mode/sql"),
    (m) => m.standardSQL,
  ),
  toml: legacy(
    () => import("@codemirror/legacy-modes/mode/toml"),
    (m) => m.toml,
  ),
  typescript: legacy(jsModes, (m) => m.typescript),
  xml: legacy(xmlModes, (m) => m.xml),
  yaml: () => import("@codemirror/lang-yaml").then(({ yaml }) => yaml()),
};
