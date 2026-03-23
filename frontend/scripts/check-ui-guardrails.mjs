/**
 * Guardrails for the selective MUI-removal restart.
 *
 * Legacy MUI/Emotion usage still exists in the repo. This script snapshots the
 * current counts into an allowlist so future changes cannot silently increase
 * those counts or introduce new frozen-wrapper imports.
 *
 * Usage:
 *   node scripts/check-ui-guardrails.mjs
 *   node scripts/check-ui-guardrails.mjs --write-allowlist
 */

import { readdirSync, readFileSync, writeFileSync } from "fs";
import { extname, join, relative, resolve } from "path";
import { fileURLToPath } from "url";
import ts from "typescript";

const __dirname = resolve(fileURLToPath(new URL(".", import.meta.url)));
const frontendDir = resolve(__dirname, "..");
const sourceRoot = resolve(frontendDir, "src");
const allowlistPath = resolve(__dirname, "ui-guardrails-allowlist.json");
const sourceExtensions = new Set([".js", ".jsx", ".ts", ".tsx"]);
const frozenWrapperNames = new Set([
  "AppGrid",
  "AppPaper",
  "AppTypography",
  "AppChip",
  "AppSelect",
]);

function getScriptKind(filePath) {
  switch (extname(filePath).toLowerCase()) {
    case ".js":
      return ts.ScriptKind.JS;
    case ".jsx":
      return ts.ScriptKind.JSX;
    case ".ts":
      return ts.ScriptKind.TS;
    case ".tsx":
      return ts.ScriptKind.TSX;
    default:
      return ts.ScriptKind.Unknown;
  }
}

function isSourceFile(filePath) {
  const extension = extname(filePath);
  return sourceExtensions.has(extension) && !filePath.endsWith(".d.ts");
}

function walkFiles(dir) {
  const entries = readdirSync(dir, { withFileTypes: true }).sort((left, right) =>
    left.name.localeCompare(right.name),
  );
  const files = [];

  for (const entry of entries) {
    const fullPath = join(dir, entry.name);

    if (entry.isDirectory()) {
      files.push(...walkFiles(fullPath));
      continue;
    }

    if (isSourceFile(fullPath)) {
      files.push(fullPath);
    }
  }

  return files;
}

function addCount(map, key, metadata, amount = 1) {
  const current = map.get(key);

  if (current) {
    current.count += amount;
    return;
  }

  map.set(key, { ...metadata, count: amount });
}

function normalizeImportSource(source) {
  if (source.startsWith("@mui/")) {
    return { type: "import-mui", source };
  }

  if (source.startsWith("@emotion/")) {
    return { type: "import-emotion", source };
  }

  return null;
}

function getFrozenWrapperName(source) {
  for (const wrapperName of frozenWrapperNames) {
    if (
      source === `@/components/ui/${wrapperName}` ||
      source.endsWith(`/components/ui/${wrapperName}`) ||
      source.endsWith(`/${wrapperName}`) ||
      source === `./${wrapperName}` ||
      source === `../${wrapperName}`
    ) {
      return wrapperName;
    }
  }

  return null;
}

function analyzeFile(filePath) {
  const relativePath = relative(frontendDir, filePath).replaceAll("\\", "/");
  const sourceText = readFileSync(filePath, "utf-8");
  const sourceFile = ts.createSourceFile(
    filePath,
    sourceText,
    ts.ScriptTarget.Latest,
    true,
    getScriptKind(filePath),
  );
  const entries = new Map();

  function visit(node) {
    if (ts.isImportDeclaration(node) && ts.isStringLiteral(node.moduleSpecifier)) {
      const source = node.moduleSpecifier.text;
      const normalized = normalizeImportSource(source);

      if (normalized) {
        const key = `${normalized.type}:${relativePath}:${normalized.source}`;
        addCount(entries, key, {
          type: normalized.type,
          file: relativePath,
          source: normalized.source,
        });
      }

      const frozenWrapperName = getFrozenWrapperName(source);

      if (frozenWrapperName) {
        const key = `import-frozen-wrapper:${relativePath}:${frozenWrapperName}`;
        addCount(entries, key, {
          type: "import-frozen-wrapper",
          file: relativePath,
          source: frozenWrapperName,
        });
      }
    }

    ts.forEachChild(node, visit);
  }

  visit(sourceFile);

  const sxMatches = sourceText.match(/\bsx\s*=/g);
  if (sxMatches?.length) {
    addCount(
      entries,
      `jsx-sx:${relativePath}`,
      {
        type: "jsx-sx",
        file: relativePath,
        source: "sx=",
      },
      sxMatches.length,
    );
  }

  const styledMatches = sourceText.match(/\bstyled\s*\(/g);
  if (styledMatches?.length) {
    addCount(
      entries,
      `styled-call:${relativePath}`,
      {
        type: "styled-call",
        file: relativePath,
        source: "styled(",
      },
      styledMatches.length,
    );
  }

  return [...entries.values()];
}

function collectEntries() {
  const files = walkFiles(sourceRoot);

  return files
    .flatMap((filePath) => analyzeFile(filePath))
    .sort((left, right) => {
      if (left.type !== right.type) {
        return left.type.localeCompare(right.type);
      }

      if (left.file !== right.file) {
        return left.file.localeCompare(right.file);
      }

      return String(left.source).localeCompare(String(right.source));
    });
}

function writeAllowlist(entries) {
  writeFileSync(
    allowlistPath,
    `${JSON.stringify(
      {
        version: 1,
        generatedAt: new Date().toISOString(),
        entries,
      },
      null,
      2,
    )}\n`,
  );
}

function buildAllowlistMap(allowlist) {
  const map = new Map();

  for (const entry of allowlist.entries ?? []) {
    const key = `${entry.type}:${entry.file}:${entry.source ?? ""}`;
    map.set(key, entry.count);
  }

  return map;
}

function printFailures(failures) {
  console.error("UI guardrails failed.\n");

  for (const failure of failures) {
    console.error(
      `- ${failure.type} in ${failure.file}${failure.source ? ` (${failure.source})` : ""}: current ${failure.count}, allowed ${failure.allowed}`,
    );
  }
}

function main() {
  const writeAllowlistMode = process.argv.includes("--write-allowlist");
  const entries = collectEntries();

  if (writeAllowlistMode) {
    writeAllowlist(entries);
    console.log(
      `Wrote ${entries.length} UI guardrail allowlist entries to ${relative(frontendDir, allowlistPath)}`,
    );
    return;
  }

  const allowlist = JSON.parse(readFileSync(allowlistPath, "utf-8"));
  const allowlistMap = buildAllowlistMap(allowlist);
  const failures = [];

  for (const entry of entries) {
    const key = `${entry.type}:${entry.file}:${entry.source ?? ""}`;
    const allowed = allowlistMap.get(key) ?? 0;

    if (entry.count > allowed) {
      failures.push({
        ...entry,
        allowed,
      });
    }
  }

  if (failures.length > 0) {
    printFailures(failures);
    process.exitCode = 1;
    return;
  }

  console.log(`UI guardrails passed with ${entries.length} tracked legacy entries.`);
}

main();
