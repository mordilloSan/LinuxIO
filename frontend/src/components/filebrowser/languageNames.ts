// Kept free of CodeMirror imports so Markdown rendering can name a language
// without pulling the editor stack into the initial chunk.
export type LanguageName =
  | "c"
  | "cpp"
  | "css"
  | "diff"
  | "dockerfile"
  | "go"
  | "html"
  | "ini"
  | "java"
  | "javascript"
  | "json"
  | "lua"
  | "perl"
  | "python"
  | "ruby"
  | "rust"
  | "scss"
  | "shell"
  | "sql"
  | "text"
  | "toml"
  | "typescript"
  | "xml"
  | "yaml";

const fenceAliases: Record<string, LanguageName> = {
  bash: "shell",
  css: "css",
  diff: "diff",
  dockerfile: "dockerfile",
  go: "go",
  html: "html",
  ini: "ini",
  javascript: "javascript",
  js: "javascript",
  json: "json",
  lua: "lua",
  perl: "perl",
  properties: "ini",
  py: "python",
  python: "python",
  rb: "ruby",
  ruby: "ruby",
  sh: "shell",
  shell: "shell",
  sql: "sql",
  toml: "toml",
  ts: "typescript",
  typescript: "typescript",
  xml: "xml",
  yaml: "yaml",
  yml: "yaml",
  zsh: "shell",
};

/** Maps a Markdown fence info string ("js", "bash title=x") to a language. */
export function languageForFence(info: string): LanguageName | null {
  const name = info.trim().split(/\s+/)[0]?.toLowerCase() ?? "";
  return Object.hasOwn(fenceAliases, name) ? fenceAliases[name] : null;
}
