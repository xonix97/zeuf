import { theme, symbols } from "../theme";
import { padRight, padCenter, visibleWidth, truncate } from "../utils";

export interface ChatBlock {
  type: "user" | "assistant" | "thinking" | "tool" | "system" | "error";
  text: string;
  toolName?: string;
  toolArgs?: string;
  toolOk?: boolean;
  durationMs?: number;
  thinkDuration?: number;
  timestamp?: string;
  model?: string;
}

export class ChatView {
  blocks: ChatBlock[] = [];
  scrollOffset: number = 0;

  append(block: ChatBlock): void {
    if (!block.timestamp) {
      const now = new Date();
      block.timestamp = now.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
    }
    this.blocks.push(block);
  }

  render(width: number, height: number, activeModel = "auto", branch = "master"): string[] {
    const rawLines: string[] = [];
    const maxContentWidth = Math.max(30, width - 4);

    if (this.blocks.length === 0) {
      // Render Rich Empty State Hero Banner
      rawLines.push("");
      rawLines.push(padCenter(theme.bold(theme.accent("◈   Z E U F   A R C H I T E C T   ◈")), maxContentWidth));
      rawLines.push(padCenter(theme.dim("Autonomous AI Engineering Agent • High-Performance Bun Runtime"), maxContentWidth));
      rawLines.push(padCenter(theme.dim(`Connected: ${theme.green(activeModel)}  •  Branch: ${theme.orange(branch)}  •  v0.5.0`), maxContentWidth));
      rawLines.push("");

      const cardWidth = 36;
      const colGap = Math.max(2, maxContentWidth - (cardWidth * 2));
      const innerW = cardWidth - 2;

      if (maxContentWidth >= 74) {
        const capTitle = `╭─ ${symbols.zap} Capabilities `;
        const capBar = "─".repeat(Math.max(0, cardWidth - visibleWidth(capTitle) - 1)) + "╮";
        const capBox = [
          theme.borderActive(capTitle) + theme.borderActive(capBar),
          theme.borderActive("│ ") + padRight(theme.white("• Autonomous code edits"), innerW - 1) + theme.borderActive("│"),
          theme.borderActive("│ ") + padRight(theme.white("• Shell & git automation"), innerW - 1) + theme.borderActive("│"),
          theme.borderActive("│ ") + padRight(theme.white("• Fast-path direct chat"), innerW - 1) + theme.borderActive("│"),
          theme.borderActive("│ ") + padRight(theme.white("• Multi-model fallback"), innerW - 1) + theme.borderActive("│"),
          theme.borderActive("╰" + "─".repeat(innerW) + "╯"),
        ];

        const keyTitle = `╭─ ${symbols.key} Shortcuts `;
        const keyBar = "─".repeat(Math.max(0, cardWidth - visibleWidth(keyTitle) - 1)) + "╮";
        const keyBox = [
          theme.borderActive(keyTitle) + theme.borderActive(keyBar),
          theme.borderActive("│ ") + padRight(theme.orange("[ / ] ") + theme.dim("Command palette"), innerW - 1) + theme.borderActive("│"),
          theme.borderActive("│ ") + padRight(theme.orange("[^P] ") + theme.dim("Switch AI model"), innerW - 1) + theme.borderActive("│"),
          theme.borderActive("│ ") + padRight(theme.orange("[^C] ") + theme.dim("Cancel/interrupt"), innerW - 1) + theme.borderActive("│"),
          theme.borderActive("│ ") + padRight(theme.orange("[Esc] ") + theme.dim("Close overlays"), innerW - 1) + theme.borderActive("│"),
          theme.borderActive("╰" + "─".repeat(innerW) + "╯"),
        ];

        for (let i = 0; i < capBox.length; i++) {
          const l = capBox[i];
          const r = keyBox[i];
          const combined = l + " ".repeat(colGap) + r;
          rawLines.push(padCenter(combined, maxContentWidth));
        }
      }

      rawLines.push("");
      rawLines.push(padCenter(theme.dim(`${symbols.bulb} Type your task or question below, or type `) + theme.orange("/") + theme.dim(" for commands"), maxContentWidth));
      rawLines.push("");
    } else {
      // Render Conversation Cards
      for (const b of this.blocks) {
        if (b.type === "user") {
          rawLines.push("");
          const title = ` ${symbols.user} You  `;
          const time = b.timestamp ? ` ${b.timestamp} ` : "";
          const barLen = Math.max(0, maxContentWidth - visibleWidth(title) - visibleWidth(time) - 4);
          rawLines.push(theme.border("╭─") + theme.bold(theme.orange(title)) + theme.border("─".repeat(barLen)) + theme.dim(time) + theme.border("─╮"));

          for (const line of b.text.split("\n")) {
            rawLines.push(theme.border("│  ") + padRight(theme.bold(theme.white(line)), maxContentWidth - 4) + theme.border("│"));
          }
          rawLines.push(theme.border("╰" + "─".repeat(maxContentWidth - 2) + "╯"));
        } else if (b.type === "assistant") {
          rawLines.push("");
          const modelTag = b.model ? ` [${b.model}] ` : "";
          const title = ` ${symbols.brand} Zeuf ${modelTag} `;
          const time = b.timestamp ? ` ${b.timestamp} ` : "";
          const barLen = Math.max(0, maxContentWidth - visibleWidth(title) - visibleWidth(time) - 4);
          rawLines.push(theme.borderActive("╭─") + theme.bold(theme.accent(title)) + theme.border("─".repeat(barLen)) + theme.dim(time) + theme.borderActive("─╮"));

          for (const line of b.text.split("\n")) {
            rawLines.push(theme.borderActive("│  ") + padRight(line, maxContentWidth - 4) + theme.borderActive("│"));
          }
          rawLines.push(theme.borderActive("╰" + "─".repeat(maxContentWidth - 2) + "╯"));
        } else if (b.type === "thinking") {
          const durStr = b.thinkDuration ? `Thought for ${(b.thinkDuration / 1000).toFixed(1)}s` : "Thinking…";
          rawLines.push(theme.dim(`  ${symbols.hollow} ${durStr}`));
          if (b.text && b.text.trim()) {
            for (const line of b.text.trim().split("\n").slice(0, 4)) {
              rawLines.push(theme.dim("  │ " + line));
            }
          }
        } else if (b.type === "tool") {
          const icon = b.toolOk === undefined ? theme.orange("●") : b.toolOk ? theme.green("✓") : theme.red("✗");
          const statusBadge = b.toolOk === undefined
            ? theme.badgeWarning("RUNNING")
            : b.toolOk
            ? theme.badgeSuccess("SUCCESS")
            : theme.badgeError("FAILED");
          const dur = b.durationMs !== undefined ? theme.dim(` (${b.durationMs}ms)`) : "";

          rawLines.push("");
          rawLines.push(theme.border("  ╭─ ") + `${icon} ${theme.bold(b.toolName || "tool")}${dur} ` + theme.border("─ ") + statusBadge);
          if (b.toolArgs) {
            rawLines.push(theme.border("  │ ") + padRight(theme.dim(`args: ${truncate(b.toolArgs, maxContentWidth - 14)}`), maxContentWidth - 4) + theme.border("│"));
          }
          if (b.text && b.text.trim()) {
            for (const line of b.text.trim().split("\n").slice(0, 5)) {
              rawLines.push(theme.border("  │ ") + padRight(theme.dim(line), maxContentWidth - 4) + theme.border("│"));
            }
          }
          rawLines.push(theme.border("  ╰─" + "─".repeat(maxContentWidth - 4) + "╯"));
        } else if (b.type === "error") {
          rawLines.push("");
          rawLines.push(theme.badgeError("ERROR") + " " + theme.red(b.text));
        } else if (b.type === "system") {
          rawLines.push(theme.dim(`  ${symbols.spark} ${b.text}`));
        }
      }
    }

    // Window scroll viewport to bottom
    const visibleLines = rawLines.slice(Math.max(0, rawLines.length - height));
    return visibleLines;
  }
}
