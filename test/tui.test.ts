import { describe, it, expect } from "bun:test";
import { visibleWidth, padRight, padCenter, truncate } from "../src/tui/utils";
import { CommandPopup } from "../src/tui/components/popup";
import { ChatView } from "../src/tui/components/chat";

describe("Zeuf TUI Layer", () => {
  it("computes visible width accurately ignoring ANSI color codes", () => {
    const plain = "Hello Zeuf";
    const colored = `\x1b[31m\x1b[1mHello Zeuf\x1b[0m`;
    expect(visibleWidth(plain)).toBe(10);
    expect(visibleWidth(colored)).toBe(10);
  });

  it("pads strings to exact target column widths", () => {
    const colored = `\x1b[32mTarget\x1b[0m`;
    const padded = padRight(colored, 20);
    expect(visibleWidth(padded)).toBe(20);
  });

  it("renders welcome hero screen when chat is empty", () => {
    const chat = new ChatView();
    const lines = chat.render(80, 20, "opencode/big-pickle", "master");
    expect(lines.length).toBeGreaterThan(5);

    const fullText = lines.join("\n");
    expect(fullText).toContain("Z E U F");
    expect(fullText).toContain("Capabilities");
    expect(fullText).toContain("Shortcuts");
  });

  it("filters and renders slash commands popup with clean padding", () => {
    const popup = new CommandPopup();
    const filtered = popup.filter("/mod");
    expect(filtered.length).toBe(1);
    expect(filtered[0].name).toBe("/models");

    const lines = popup.render(filtered, 80);
    expect(lines.length).toBeGreaterThanOrEqual(3);
    for (const line of lines) {
      expect(visibleWidth(line)).toBeLessThanOrEqual(80);
    }
  });
});
