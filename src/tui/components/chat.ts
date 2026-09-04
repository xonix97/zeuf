import { theme } from "../theme";

export interface ChatBlock {
  type: "user" | "assistant" | "thinking" | "tool" | "system" | "error";
  text: string;
  toolName?: string;
  toolArgs?: string;
  toolOk?: boolean;
  durationMs?: number;
  thinkDuration?: number;
}

export class ChatView {
  blocks: ChatBlock[] = [];
  scrollOffset: number = 0;

  append(block: ChatBlock): void {
    this.blocks.push(block);
  }

  render(width: number, height: number): string[] {
    const lines: string[] = [];
    const contentWidth = Math.max(20, width - 2);

    for (const b of this.blocks) {
      if (b.type === "user") {
        lines.push("");
        lines.push(theme.orange("› ") + theme.bold(theme.white(b.text)));
      } else if (b.type === "assistant") {
        lines.push("");
        for (const line of b.text.split("\n")) {
          lines.push("  " + line);
        }
      } else if (b.type === "thinking") {
        lines.push("");
        const durStr = b.thinkDuration ? `Thought for ${(b.thinkDuration / 1000).toFixed(1)}s` : "Thinking…";
        lines.push(theme.dim(`◌ ${durStr}`));
        if (b.text.trim()) {
          for (const line of b.text.trim().split("\n").slice(0, 5)) {
            lines.push(theme.dim("  " + line));
          }
        }
      } else if (b.type === "tool") {
        const icon = b.toolOk === undefined ? theme.orange("●") : b.toolOk ? theme.green("✓") : theme.red("✗");
        const dur = b.durationMs !== undefined ? ` (${b.durationMs}ms)` : "";
        lines.push(`  ${icon} ${theme.bold(b.toolName || "tool")}${dur}`);
        if (b.text.trim()) {
          for (const line of b.text.trim().split("\n").slice(0, 4)) {
            lines.push(theme.dim("    " + line));
          }
        }
      } else if (b.type === "error") {
        lines.push("");
        lines.push(theme.red("✗ Error: ") + b.text);
      }
    }

    if (lines.length === 0) {
      lines.push("");
      lines.push(theme.dim("  Ask Zeuf to inspect code, edit files, build features, or run commands."));
      lines.push(theme.dim("  Type / for commands, Ctrl+P to select model."));
    }

    // Scroll viewport to bottom
    const visibleLines = lines.slice(Math.max(0, lines.length - height));
    return visibleLines;
  }
}
