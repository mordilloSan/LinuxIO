import { describe, expect, it } from "vitest";

import { languageForFence } from "@/components/filebrowser/languageNames";

import { highlightCode } from "./highlight";

describe("languageForFence", () => {
  it("maps fence info strings to editor languages", () => {
    expect(languageForFence("bash")).toBe("shell");
    expect(languageForFence("SH")).toBe("shell");
    expect(languageForFence("yml")).toBe("yaml");
    expect(languageForFence("py")).toBe("python");
    expect(languageForFence("ts")).toBe("typescript");
    expect(languageForFence("properties")).toBe("ini");
    expect(languageForFence("js title=a.js")).toBe("javascript");
  });

  it("returns null for unknown or empty info", () => {
    expect(languageForFence("")).toBeNull();
    expect(languageForFence("brainfuck")).toBeNull();
    expect(languageForFence("constructor")).toBeNull();
  });
});

describe("highlightCode", () => {
  it("classes tokens and preserves the text", async () => {
    const code = "const x = 1";
    const spans = await highlightCode(code, "javascript");
    expect(spans.map((s) => s.text).join("")).toBe(code);
    expect(spans.find((s) => s.text === "const")?.className).toContain(
      "tok-keyword",
    );
  });

  it("highlights a StreamLanguage-free language (json) too", async () => {
    const spans = await highlightCode('{"a": 1}', "json");
    expect(spans.some((s) => s.className?.includes("tok-propertyName"))).toBe(
      true,
    );
  });
});
