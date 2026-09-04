import React from "react";
import { Box, Text } from "ink";
import { editorial } from "../editorialTheme";

export interface MarkdownViewProps {
  content: string;
}

/**
 * Render math & LaTeX symbols to Unicode equivalents
 */
export function renderMathSymbols(text: string): string {
  return text
    // Fractions: \frac{a}{b} -> (a / b)
    .replace(/\\frac\s*\{([^}]+)\}\s*\{([^}]+)\}/g, "($1 / $2)")
    .replace(/\\frac\s+([a-zA-Z0-9]+)\s+([a-zA-Z0-9]+)/g, "($1 / $2)")
    .replace(/\\frac\b/g, "÷")
    // Square root: \sqrt{x} -> √(x), \sqrt x -> √x, \sqrt -> √
    .replace(/\\sqrt\s*\{([^}]+)\}/g, "√($1)")
    .replace(/\\sqrt\s+([a-zA-Z0-9]+)/g, "√$1")
    .replace(/\\sqrt\b/g, "√")
    // Common operators
    .replace(/\\times\b/g, "×")
    .replace(/\\div\b/g, "÷")
    .replace(/\\pm\b/g, "±")
    .replace(/\\mp\b/g, "∓")
    .replace(/\\cdot\b/g, "·")
    .replace(/\\approx\b/g, "≈")
    .replace(/\\neq\b/g, "≠")
    .replace(/\\ne\b/g, "≠")
    .replace(/\\leq\b/g, "≤")
    .replace(/\\le\b/g, "≤")
    .replace(/\\geq\b/g, "≥")
    .replace(/\\ge\b/g, "≥")
    .replace(/\\infty\b/g, "∞")
    .replace(/\\sum\b/g, "∑")
    .replace(/\\prod\b/g, "∏")
    .replace(/\\int\b/g, "∫")
    .replace(/\\partial\b/g, "∂")
    .replace(/\\nabla\b/g, "∇")
    // Greek alphabet
    .replace(/\\alpha\b/g, "α")
    .replace(/\\beta\b/g, "β")
    .replace(/\\gamma\b/g, "γ")
    .replace(/\\delta\b/g, "δ")
    .replace(/\\epsilon\b/g, "ε")
    .replace(/\\theta\b/g, "θ")
    .replace(/\\lambda\b/g, "λ")
    .replace(/\\mu\b/g, "μ")
    .replace(/\\pi\b/g, "π")
    .replace(/\\rho\b/g, "ρ")
    .replace(/\\sigma\b/g, "σ")
    .replace(/\\tau\b/g, "τ")
    .replace(/\\phi\b/g, "φ")
    .replace(/\\omega\b/g, "ω")
    .replace(/\\Delta\b/g, "Δ")
    .replace(/\\Sigma\b/g, "Σ")
    .replace(/\\Omega\b/g, "Ω")
    // Arrows
    .replace(/\\rightarrow\b/g, "→")
    .replace(/\\leftarrow\b/g, "←")
    .replace(/\\Rightarrow\b/g, "⇒")
    .replace(/\\Leftarrow\b/g, "⇐")
    .replace(/\\leftrightarrow\b/g, "↔")
    // Superscripts
    .replace(/\^0/g, "⁰")
    .replace(/\^1/g, "¹")
    .replace(/\^2/g, "²")
    .replace(/\^3/g, "³")
    .replace(/\^4/g, "⁴")
    .replace(/\^5/g, "⁵")
    .replace(/\^6/g, "⁶")
    .replace(/\^7/g, "⁷")
    .replace(/\^8/g, "⁸")
    .replace(/\^9/g, "⁹")
    .replace(/\^n/g, "ⁿ")
    .replace(/\^x/g, "ˣ");
}

export interface InlineToken {
  type: "code" | "bold_italic" | "bold" | "italic" | "text";
  text: string;
}

/**
 * Tokenize a line into inlines: inline code, bold, italic, and text
 */
export function tokenizeInlines(rawText: string): InlineToken[] {
  const text = renderMathSymbols(rawText);
  const tokens: InlineToken[] = [];
  let index = 0;

  while (index < text.length) {
    // 1. Inline code: `code`
    if (text[index] === "`") {
      const closing = text.indexOf("`", index + 1);
      if (closing !== -1) {
        tokens.push({
          type: "code",
          text: text.slice(index + 1, closing),
        });
        index = closing + 1;
        continue;
      }
    }

    // 2. Bold + Italic: ***text*** or ___text___
    if (
      (text.startsWith("***", index) || text.startsWith("___", index)) &&
      text.length > index + 3
    ) {
      const delim = text.slice(index, index + 3);
      const closing = text.indexOf(delim, index + 3);
      if (closing !== -1) {
        tokens.push({
          type: "bold_italic",
          text: text.slice(index + 3, closing),
        });
        index = closing + 3;
        continue;
      }
    }

    // 3. Bold: **text** or __text__
    if (
      (text.startsWith("**", index) || text.startsWith("__", index)) &&
      text.length > index + 2
    ) {
      const delim = text.slice(index, index + 2);
      const closing = text.indexOf(delim, index + 2);
      if (closing !== -1) {
        tokens.push({
          type: "bold",
          text: text.slice(index + 2, closing),
        });
        index = closing + 2;
        continue;
      }
    }

    // 4. Italic: *text* or _text_
    if (
      (text[index] === "*" || text[index] === "_") &&
      index + 1 < text.length &&
      text[index + 1] !== " "
    ) {
      const delim = text[index];
      const closing = text.indexOf(delim, index + 1);
      if (closing !== -1 && text[closing - 1] !== " ") {
        tokens.push({
          type: "italic",
          text: text.slice(index + 1, closing),
        });
        index = closing + 1;
        continue;
      }
    }

    // Accumulate plain text
    let nextSpecial = text.length;
    for (const spec of ["`", "**", "__", "***", "___", "*", "_"]) {
      const pos = text.indexOf(spec, index);
      if (pos !== -1 && pos < nextSpecial) {
        nextSpecial = pos;
      }
    }

    if (nextSpecial > index) {
      tokens.push({
        type: "text",
        text: text.slice(index, nextSpecial),
      });
      index = nextSpecial;
    } else {
      tokens.push({
        type: "text",
        text: text[index],
      });
      index++;
    }
  }

  return tokens;
}

/**
 * Render inline tokens into Ink Text elements
 */
export const InlineText: React.FC<{ text: string }> = ({ text }) => {
  const tokens = tokenizeInlines(text);

  return (
    <Text color={editorial.creamSoft}>
      {tokens.map((token, idx) => {
        if (token.type === "code") {
          return (
            <Text key={idx} bold color={editorial.gold}>
              `{token.text}`
            </Text>
          );
        }
        if (token.type === "bold_italic") {
          return (
            <Text key={idx} bold italic color={editorial.cream}>
              {token.text}
            </Text>
          );
        }
        if (token.type === "bold") {
          return (
            <Text key={idx} bold color={editorial.cream}>
              {token.text}
            </Text>
          );
        }
        if (token.type === "italic") {
          return (
            <Text key={idx} italic color={editorial.creamDim}>
              {token.text}
            </Text>
          );
        }
        return <Text key={idx}>{token.text}</Text>;
      })}
    </Text>
  );
};

export interface BlockItem {
  type: "heading" | "code_block" | "list_item" | "quote" | "hr" | "paragraph";
  level?: number;
  lang?: string;
  lines: string[];
  number?: string;
}

/**
 * Parse markdown string into structural blocks
 */
export function parseBlocks(markdown: string): BlockItem[] {
  const lines = markdown.split("\n");
  const blocks: BlockItem[] = [];
  let i = 0;

  while (i < lines.length) {
    const rawLine = lines[i];
    const trimmed = rawLine.trim();

    // 1. Fenced Code Block
    if (trimmed.startsWith("```") || trimmed.startsWith("~~~")) {
      const fence = trimmed.slice(0, 3);
      const lang = trimmed.slice(3).trim();
      const codeLines: string[] = [];
      i++;
      while (i < lines.length && !lines[i].trim().startsWith(fence)) {
        codeLines.push(lines[i]);
        i++;
      }
      if (i < lines.length) i++; // consume closing fence

      blocks.push({
        type: "code_block",
        lang: lang || "code",
        lines: codeLines,
      });
      continue;
    }

    // 2. Horizontal Rule
    if (/^(?:---|\*\*\*|___)\s*$/.test(trimmed)) {
      blocks.push({ type: "hr", lines: [] });
      i++;
      continue;
    }

    // 3. Headings (# H1, ## H2, ### H3, #### H4)
    const headingMatch = rawLine.match(/^(#{1,6})\s+(.+)$/);
    if (headingMatch) {
      blocks.push({
        type: "heading",
        level: headingMatch[1].length,
        lines: [headingMatch[2]],
      });
      i++;
      continue;
    }

    // 4. Blockquote (> text)
    if (trimmed.startsWith(">")) {
      const quoteLines: string[] = [];
      while (i < lines.length && lines[i].trim().startsWith(">")) {
        quoteLines.push(lines[i].trim().replace(/^>\s*/, ""));
        i++;
      }
      blocks.push({
        type: "quote",
        lines: quoteLines,
      });
      continue;
    }

    // 5. Unordered list (- item, * item, + item)
    const bulletMatch = rawLine.match(/^(\s*)([-*+])\s+(.+)$/);
    if (bulletMatch) {
      blocks.push({
        type: "list_item",
        lines: [bulletMatch[3]],
      });
      i++;
      continue;
    }

    // 6. Ordered list (1. item, 2. item)
    const numMatch = rawLine.match(/^(\s*)(\d+)\.\s+(.+)$/);
    if (numMatch) {
      blocks.push({
        type: "list_item",
        number: numMatch[2],
        lines: [numMatch[3]],
      });
      i++;
      continue;
    }

    // 7. Empty lines
    if (!trimmed) {
      i++;
      continue;
    }

    // 8. Normal paragraph lines (accumulate non-empty lines until a block boundary)
    const paraLines: string[] = [rawLine];
    i++;
    while (
      i < lines.length &&
      lines[i].trim() &&
      !lines[i].trim().startsWith("```") &&
      !lines[i].trim().startsWith("~~~") &&
      !lines[i].trim().startsWith("#") &&
      !lines[i].trim().startsWith(">") &&
      !/^(\s*)([-*+]|\d+\.)\s+/.test(lines[i]) &&
      !/^(?:---|\*\*\*|___)\s*$/.test(lines[i].trim())
    ) {
      paraLines.push(lines[i]);
      i++;
    }

    blocks.push({
      type: "paragraph",
      lines: paraLines,
    });
  }

  return blocks;
}

/**
 * Basic syntax highlighting for code lines
 */
export function highlightCodeLine(line: string): React.ReactNode {
  if (!line) return <Text> </Text>;

  // Comments
  if (line.trim().startsWith("//") || line.trim().startsWith("#")) {
    return <Text color={editorial.creamMute}>{line}</Text>;
  }

  return <Text color={editorial.creamSoft}>{line}</Text>;
}

export const MarkdownView: React.FC<MarkdownViewProps> = ({ content }) => {
  if (!content || !content.trim()) return null;

  const blocks = parseBlocks(content);

  return (
    <Box flexDirection="column" width="100%">
      {blocks.map((block, idx) => {
        // Heading
        if (block.type === "heading") {
          const text = block.lines.join(" ");
          if (block.level === 1) {
            return (
              <Box key={idx} marginY={1} flexDirection="column">
                <Box gap={1}>
                  <Text bold backgroundColor={editorial.paper} color={editorial.ink}>
                    {" "}#{" "}
                  </Text>
                  <Text bold color={editorial.cream}>
                    {renderMathSymbols(text)}
                  </Text>
                </Box>
              </Box>
            );
          }
          if (block.level === 2) {
            return (
              <Box key={idx} marginTop={1} gap={1}>
                <Text bold color={editorial.gold}>##</Text>
                <Text bold color={editorial.cream}>
                  {renderMathSymbols(text)}
                </Text>
              </Box>
            );
          }
          if (block.level === 3) {
            return (
              <Box key={idx} marginTop={1} gap={1}>
                <Text bold color={editorial.sage}>###</Text>
                <Text bold color={editorial.creamSoft}>
                  {renderMathSymbols(text)}
                </Text>
              </Box>
            );
          }
          return (
            <Box key={idx} marginTop={1} gap={1}>
              <Text bold color={editorial.creamDim}>####</Text>
              <Text bold color={editorial.creamDim}>
                {renderMathSymbols(text)}
              </Text>
            </Box>
          );
        }

        // Fenced Code Block
        if (block.type === "code_block") {
          return (
            <Box
              key={idx}
              borderStyle="round"
              borderColor={editorial.line2}
              flexDirection="column"
              paddingX={1}
              marginY={1}
              width="100%"
            >
              <Box justifyContent="space-between" marginBottom={block.lines.length > 0 ? 1 : 0}>
                <Text bold color={editorial.gold}>
                  {block.lang?.toUpperCase() || "CODE"}
                </Text>
                <Text color={editorial.creamMute}>copy</Text>
              </Box>
              {block.lines.map((l, lineIdx) => (
                <Box key={lineIdx} gap={1}>
                  <Text color={editorial.creamMute}>
                    {(lineIdx + 1).toString().padStart(2, " ")} │
                  </Text>
                  {highlightCodeLine(l)}
                </Box>
              ))}
            </Box>
          );
        }

        // Horizontal Rule
        if (block.type === "hr") {
          return (
            <Box key={idx} marginY={1}>
              <Text color={editorial.line2}>{"─".repeat(50)}</Text>
            </Box>
          );
        }

        // Quote
        if (block.type === "quote") {
          return (
            <Box key={idx} marginY={0} paddingLeft={1} gap={1}>
              <Text color={editorial.line2}>│</Text>
              <Box flexDirection="column">
                {block.lines.map((ql, qIdx) => (
                  <InlineText key={qIdx} text={ql} />
                ))}
              </Box>
            </Box>
          );
        }

        // List Item
        if (block.type === "list_item") {
          const prefix = block.number ? `${block.number}. ` : "• ";
          return (
            <Box key={idx} paddingLeft={1} gap={1}>
              <Text color={editorial.gold}>{prefix}</Text>
              <InlineText text={block.lines.join(" ")} />
            </Box>
          );
        }

        // Paragraph
        return (
          <Box key={idx} marginY={0} flexDirection="column">
            {block.lines.map((line, lineIdx) => (
              <InlineText key={lineIdx} text={line} />
            ))}
          </Box>
        );
      })}
    </Box>
  );
};
