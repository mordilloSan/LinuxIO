/**
 * Audits direct MUI usage in the frontend source tree.
 *
 * Usage:
 *   node scripts/count-mui-usage.mjs
 *   node scripts/count-mui-usage.mjs --json
 *   node scripts/count-mui-usage.mjs --top=25
 *   node scripts/count-mui-usage.mjs --root=src/pages
 */

import { readdirSync, readFileSync } from "fs";
import { extname, join, relative, resolve } from "path";
import { fileURLToPath } from "url";
import ts from "typescript";

const __dirname = resolve(fileURLToPath(new URL(".", import.meta.url)));
const frontendDir = resolve(__dirname, "..");
const DEFAULT_ROOT = "src";
const DEFAULT_TOP = 15;
const SOURCE_EXTENSIONS = new Set([".js", ".jsx", ".ts", ".tsx"]);
const MUI_PREFIX = "@mui/";

function parseArgs(argv) {
  const options = {
    json: false,
    root: DEFAULT_ROOT,
    top: DEFAULT_TOP,
  };

  for (const arg of argv) {
    if (arg === "--json") {
      options.json = true;
      continue;
    }

    if (arg.startsWith("--root=")) {
      options.root = arg.slice("--root=".length) || DEFAULT_ROOT;
      continue;
    }

    if (arg.startsWith("--top=")) {
      const value = Number.parseInt(arg.slice("--top=".length), 10);

      if (Number.isNaN(value) || value <= 0) {
        throw new Error(`Invalid --top value: ${arg}`);
      }

      options.top = value;
      continue;
    }

    throw new Error(`Unknown argument: ${arg}`);
  }

  return options;
}

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

    if (!SOURCE_EXTENSIONS.has(extname(entry.name)) || entry.name.endsWith(".d.ts")) {
      continue;
    }

    files.push(fullPath);
  }

  return files;
}

function increment(map, key, amount = 1) {
  map.set(key, (map.get(key) ?? 0) + amount);
}

function addToSet(map, key, value) {
  if (!map.has(key)) {
    map.set(key, new Set());
  }

  map.get(key).add(value);
}

function formatCount(value) {
  return new Intl.NumberFormat("en-US").format(value);
}

function sortEntries(entries) {
  return [...entries].sort((left, right) => {
    if (right[1] !== left[1]) {
      return right[1] - left[1];
    }

    return left[0].localeCompare(right[0]);
  });
}

function getSubpathSymbol(source) {
  const segments = source.split("/");
  return segments[segments.length - 1];
}

function extractImportSymbols(source, importClause) {
  if (!importClause) {
    return [
      {
        name: "(side-effect)",
        typeOnly: false,
      },
    ];
  }

  const symbols = [];

  if (importClause.name) {
    symbols.push({
      name:
        source === "@mui/material" ||
        source === "@mui/material/styles" ||
        source === "@mui/material/colors"
          ? importClause.name.text
          : getSubpathSymbol(source),
      typeOnly: importClause.isTypeOnly,
    });
  }

  if (!importClause.namedBindings) {
    return symbols;
  }

  if (ts.isNamespaceImport(importClause.namedBindings)) {
    symbols.push({
      name: "*",
      typeOnly: importClause.isTypeOnly,
    });
    return symbols;
  }

  for (const element of importClause.namedBindings.elements) {
    symbols.push({
      name: element.propertyName?.text ?? element.name.text,
      typeOnly: importClause.isTypeOnly || element.isTypeOnly,
    });
  }

  return symbols;
}

function analyzeFile(filePath) {
  const sourceText = readFileSync(filePath, "utf-8");
  const sourceFile = ts.createSourceFile(
    filePath,
    sourceText,
    ts.ScriptTarget.Latest,
    true,
    getScriptKind(filePath),
  );

  const references = [];

  function visit(node) {
    if (ts.isImportDeclaration(node) && ts.isStringLiteral(node.moduleSpecifier)) {
      const source = node.moduleSpecifier.text;

      if (source.startsWith(MUI_PREFIX)) {
        const symbols = extractImportSymbols(source, node.importClause);
        references.push({
          kind: "static",
          source,
          symbols,
        });
      }
    }

    if (
      ts.isCallExpression(node) &&
      node.expression.kind === ts.SyntaxKind.ImportKeyword &&
      node.arguments.length === 1 &&
      ts.isStringLiteral(node.arguments[0])
    ) {
      const source = node.arguments[0].text;

      if (source.startsWith(MUI_PREFIX)) {
        references.push({
          kind: "dynamic",
          source,
          symbols: [
            {
              name: "import()",
              typeOnly: false,
            },
          ],
        });
      }
    }

    ts.forEachChild(node, visit);
  }

  visit(sourceFile);
  return references;
}

function summarize(options) {
  const sourceRoot = resolve(frontendDir, options.root);
  const files = walkFiles(sourceRoot);
  const packageRefCounts = new Map();
  const packageApiCounts = new Map();
  const packageFileSets = new Map();
  const apiCounts = new Map();
  const fileStats = [];

  let filesWithMui = 0;
  let filesWithRuntimeMui = 0;
  let filesWithTypeOnlyMui = 0;
  let totalRefs = 0;
  let dynamicRefs = 0;
  let totalApis = 0;
  let runtimeApis = 0;
  let typeOnlyApis = 0;

  for (const filePath of files) {
    const references = analyzeFile(filePath);

    if (references.length === 0) {
      continue;
    }

    filesWithMui += 1;
    totalRefs += references.length;

    const relativePath = relative(frontendDir, filePath).replaceAll("\\", "/");
    let fileRuntimeApis = 0;
    let fileTypeOnlyApis = 0;

    for (const reference of references) {
      if (reference.kind === "dynamic") {
        dynamicRefs += 1;
      }

      increment(packageRefCounts, reference.source);
      addToSet(packageFileSets, reference.source, relativePath);

      for (const symbol of reference.symbols) {
        totalApis += 1;
        increment(packageApiCounts, reference.source);
        increment(apiCounts, `${reference.source}::${symbol.name}`);

        if (symbol.typeOnly) {
          typeOnlyApis += 1;
          fileTypeOnlyApis += 1;
          continue;
        }

        runtimeApis += 1;
        fileRuntimeApis += 1;
      }
    }

    if (fileRuntimeApis > 0) {
      filesWithRuntimeMui += 1;
    } else if (fileTypeOnlyApis > 0) {
      filesWithTypeOnlyMui += 1;
    }

    fileStats.push({
      path: relativePath,
      refs: references.length,
      apis: fileRuntimeApis + fileTypeOnlyApis,
      runtimeApis: fileRuntimeApis,
      typeOnlyApis: fileTypeOnlyApis,
    });
  }

  const packageBreakdown = sortEntries(packageRefCounts).map(([source, refs]) => ({
    source,
    refs,
    apis: packageApiCounts.get(source) ?? 0,
    files: packageFileSets.get(source)?.size ?? 0,
  }));

  const topApis = sortEntries(apiCounts)
    .slice(0, options.top)
    .map(([key, count]) => {
      const [source, name] = key.split("::");
      return { source, name, count };
    });

  const topFiles = [...fileStats]
    .sort((left, right) => {
      if (right.refs !== left.refs) {
        return right.refs - left.refs;
      }

      if (right.apis !== left.apis) {
        return right.apis - left.apis;
      }

      return left.path.localeCompare(right.path);
    })
    .slice(0, options.top);

  return {
    root: relative(frontendDir, sourceRoot).replaceAll("\\", "/"),
    scannedFiles: files.length,
    filesWithMui,
    filesWithRuntimeMui,
    filesWithTypeOnlyMui,
    totalRefs,
    dynamicRefs,
    totalApis,
    runtimeApis,
    typeOnlyApis,
    packageBreakdown,
    topApis,
    topFiles,
  };
}

function printTextReport(summary, top) {
  console.log(`MUI usage audit for ${summary.root}`);
  console.log(`Scanned files: ${formatCount(summary.scannedFiles)}`);
  console.log(
    `Files with MUI refs: ${formatCount(summary.filesWithMui)} (${formatCount(summary.filesWithRuntimeMui)} runtime, ${formatCount(summary.filesWithTypeOnlyMui)} type-only only)`,
  );
  console.log(
    `MUI refs: ${formatCount(summary.totalRefs)} (${formatCount(summary.dynamicRefs)} dynamic)`,
  );
  console.log(
    `Imported APIs: ${formatCount(summary.totalApis)} (${formatCount(summary.runtimeApis)} runtime, ${formatCount(summary.typeOnlyApis)} type-only)`,
  );

  console.log("\nBy source (refs | apis | files)");

  for (const item of summary.packageBreakdown) {
    console.log(
      `${String(item.refs).padStart(4)} | ${String(item.apis).padStart(4)} | ${String(item.files).padStart(4)} | ${item.source}`,
    );
  }

  console.log(`\nTop APIs (${Math.min(top, summary.topApis.length)})`);

  for (const item of summary.topApis) {
    console.log(
      `${String(item.count).padStart(4)} | ${item.source.padEnd(28)} | ${item.name}`,
    );
  }

  console.log(`\nTop files (${Math.min(top, summary.topFiles.length)})`);

  for (const item of summary.topFiles) {
    console.log(
      `${String(item.refs).padStart(4)} | ${String(item.apis).padStart(4)} | ${item.path}`,
    );
  }
}

function main() {
  const options = parseArgs(process.argv.slice(2));
  const summary = summarize(options);

  if (options.json) {
    console.log(JSON.stringify(summary, null, 2));
    return;
  }

  printTextReport(summary, options.top);
}

main();
