import { ApiResource, FileItem, FileResource } from "../../types/filebrowser";

export const normalizeResource = (data: ApiResource): FileResource => {
  if (data.type !== "directory") {
    return data;
  }

  const folders = data.folders ?? [];
  const files = data.files ?? [];

  const items: FileItem[] = [...folders, ...files].map((item) => {
    const basePath = data.path === "/" ? "/" : data.path;
    const nextPath =
      item.type === "directory"
        ? `${basePath}${item.name}/`
        : `${basePath}${item.name}`;
    const modTime = item.modTime ?? item.modified;

    return {
      ...item,
      path: nextPath.replace(/\/{2,}/g, "/"),
      modTime,
    };
  });

  return {
    ...data,
    modTime: data.modTime ?? data.modified,
    items,
  };
};

export const isArchiveFile = (name: string) => {
  const lower = name.toLowerCase();
  return (
    lower.endsWith(".zip") ||
    lower.endsWith(".tar.gz") ||
    lower.endsWith(".tgz")
  );
};

export const stripArchiveExtension = (name: string) => {
  const lower = name.toLowerCase();
  if (lower.endsWith(".tar.gz")) {
    return name.slice(0, -7);
  }
  if (lower.endsWith(".tgz")) {
    return name.slice(0, -4);
  }
  if (lower.endsWith(".zip")) {
    return name.slice(0, -4);
  }
  return name;
};

export const ensureZipExtension = (name: string) => {
  const lower = name.toLowerCase();
  if (lower.endsWith(".zip")) {
    return name;
  }
  return `${name}.zip`;
};

// Text-based file extensions that can be edited
const EDITABLE_EXTENSIONS = new Set([
  // Code
  "js",
  "ts",
  "tsx",
  "jsx",
  "mjs",
  "cjs",
  "py",
  "go",
  "cpp",
  "c",
  "h",
  "hpp",
  "java",
  "rs",
  "php",
  "rb",
  "sh",
  "bash",
  "zsh",
  "fish",
  "ps1",
  "bat",
  "cmd",
  "json",
  "html",
  "htm",
  "css",
  "scss",
  "sass",
  "less",
  "vue",
  "svelte",
  "astro",
  "sql",
  "graphql",
  "gql",
  "swift",
  "kt",
  "kts",
  "scala",
  "clj",
  "cljs",
  "ex",
  "exs",
  "erl",
  "hrl",
  "elm",
  "hs",
  "lua",
  "pl",
  "pm",
  "r",
  "dart",
  "groovy",
  "gradle",
  // Text and documentation
  "txt",
  "md",
  "markdown",
  "mdx",
  "rst",
  "log",
  "text",
  // Config files
  "yaml",
  "yml",
  "xml",
  "ini",
  "conf",
  "cfg",
  "toml",
  "env",
  "properties",
  "htaccess",
  "gitignore",
  "gitattributes",
  "dockerignore",
  "editorconfig",
  "eslintrc",
  "prettierrc",
  "babelrc",
  "npmrc",
  // Data formats
  "csv",
  "tsv",
  "jsonl",
  "ndjson",
  // Other
  "lock",
  "sum",
  "mod",
  // Dotfile configs (the part after the leading dot)
  "bashrc",
  "bash_profile",
  "bash_aliases",
  "bash_history",
  "zshrc",
  "zsh_history",
  "zprofile",
  "zshenv",
  "profile",
  "vimrc",
  "nvimrc",
  "gvimrc",
  "exrc",
  "inputrc",
  "screenrc",
  "tmux",
  "wgetrc",
  "curlrc",
  "netrc",
  "gemrc",
  "irbrc",
  "pryrc",
  "pythonrc",
  "condarc",
]);

// Files without extension that are typically editable
const EDITABLE_FILENAMES = new Set([
  "dockerfile",
  "makefile",
  "cmakelists.txt",
  "gemfile",
  "rakefile",
  "procfile",
  "vagrantfile",
  "jenkinsfile",
  "brewfile",
  "readme",
  "license",
  "changelog",
  "authors",
  "contributing",
  "todo",
  "notes",
]);

export const isEditableFile = (name: string): boolean => {
  const lower = name.toLowerCase();

  // Check if filename itself is editable (e.g., Dockerfile, Makefile)
  if (EDITABLE_FILENAMES.has(lower)) {
    return true;
  }

  // Extract extension
  const lastDotIndex = lower.lastIndexOf(".");
  if (lastDotIndex === -1 || lastDotIndex === lower.length - 1) {
    // No extension - check if it looks like a dotfile config
    if (lower.startsWith(".")) {
      // Dotfiles like .bashrc, .zshrc, .profile are typically editable
      return true;
    }
    return false;
  }

  const ext = lower.slice(lastDotIndex + 1);
  return EDITABLE_EXTENSIONS.has(ext);
};
