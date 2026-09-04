import { describe, it, expect } from "bun:test";
import React from "react";
import { render } from "ink-testing-library";
import {
  renderMathSymbols,
  tokenizeInlines,
  parseBlocks,
  MarkdownView,
} from "../src/tui/components/MarkdownView";

describe("Zeuf Markdown & Math Renderer", () => {
  it("renders math and LaTeX symbols to clean Unicode", () => {
    expect(renderMathSymbols("\\sqrt{x}")).toBe("√(x)");
    expect(renderMathSymbols("\\sqrt 2")).toBe("√2");
    expect(renderMathSymbols("\\sqrt")).toBe("√");
    expect(renderMathSymbols("\\frac{a}{b}")).toBe("(a / b)");
    expect(renderMathSymbols("\\frac 1 2")).toBe("(1 / 2)");
    expect(renderMathSymbols("\\frac")).toBe("÷");
    expect(renderMathSymbols("a \\times b = c \\div d")).toBe("a × b = c ÷ d");
    expect(renderMathSymbols("x \\approx y \\neq z \\leq w \\geq v")).toBe("x ≈ y ≠ z ≤ w ≥ v");
    expect(renderMathSymbols("\\pm \\infty \\pi \\sum \\int")).toBe("± ∞ π ∑ ∫");
    expect(renderMathSymbols("x^2 + y^3")).toBe("x² + y³");
  });

  it("tokenizes inlines: code, bold, italic, and math", () => {
    const raw = "This has `code blocks`, **bold text**, *italic text*, and \\sqrt{16}!";
    const tokens = tokenizeInlines(raw);

    expect(tokens.some(t => t.type === "code" && t.text === "code blocks")).toBe(true);
    expect(tokens.some(t => t.type === "bold" && t.text === "bold text")).toBe(true);
    expect(tokens.some(t => t.type === "italic" && t.text === "italic text")).toBe(true);
    expect(tokens.some(t => t.text.includes("√(16)"))).toBe(true);
  });

  it("parses headings, fenced code blocks, lists, and quotes", () => {
    const md = [
      "# Big Heading",
      "## Subheading",
      "### Section",
      "```typescript",
      "const answer = 42;",
      "```",
      "- Bullet 1",
      "1. Ordered 1",
      "> Quoted text",
    ].join("\n");

    const blocks = parseBlocks(md);
    expect(blocks[0].type).toBe("heading");
    expect(blocks[0].level).toBe(1);
    expect(blocks[0].lines[0]).toBe("Big Heading");

    expect(blocks[1].type).toBe("heading");
    expect(blocks[1].level).toBe(2);

    expect(blocks[2].type).toBe("heading");
    expect(blocks[2].level).toBe(3);

    expect(blocks[3].type).toBe("code_block");
    expect(blocks[3].lang).toBe("typescript");
    expect(blocks[3].lines).toEqual(["const answer = 42;"]);

    expect(blocks[4].type).toBe("list_item");
    expect(blocks[4].lines[0]).toBe("Bullet 1");

    expect(blocks[5].type).toBe("list_item");
    expect(blocks[5].number).toBe("1");

    expect(blocks[6].type).toBe("quote");
    expect(blocks[6].lines[0]).toBe("Quoted text");
  });

  it("renders user prompt markdown payload with Ink without crashing", () => {
    const userPayload = [
      "# big text",
      "## rgrsg",
      "### rsghrsi",
      "** rsgrs **",
      "* THTE *",
      "\\sqrt",
      "\\frac",
      "```",
      "copy me",
      "```",
    ].join("\n");

    const { lastFrame, unmount } = render(<MarkdownView content={userPayload} />);
    const frame = lastFrame();

    expect(frame).toContain("big text");
    expect(frame).toContain("rgrsg");
    expect(frame).toContain("rsghrsi");
    expect(frame).toContain("rsgrs");
    expect(frame).toContain("THTE");
    expect(frame).toContain("√");
    expect(frame).toContain("÷");
    expect(frame).toContain("copy me");
    expect(frame).toContain("copy");

    unmount();
  });
});
