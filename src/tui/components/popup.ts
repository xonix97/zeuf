import { theme, symbols } from "../theme";
import { visibleWidth, padRight, truncate } from "../utils";

export interface SlashCommand {
  name: string;
  icon: string;
  desc: string;
}

export const defaultSlashCommands: SlashCommand[] = [
  { name: "/models", icon: "󰘧", desc: "List and switch available AI models (OpenCode, Claude, DeepSeek)" },
  { name: "/connect", icon: "⚡", desc: "Configure model backends (Ollama localhost:11434, OpenRouter)" },
  { name: "/clear", icon: "🧹", desc: "Clear conversation history & reset viewport" },
  { name: "/sessions", icon: "📂", desc: "Browse and restore saved agent sessions" },
  { name: "/status", icon: "📊", desc: "Inspect current workspace, git diffs & tokens" },
  { name: "/help", icon: "❓", desc: "Show keyboard shortcuts & command cheat-sheet" },
  { name: "/exit", icon: "🚪", desc: "Quit Zeuf" },
];

export class CommandPopup {
  selectedIdx: number = 0;

  filter(input: string): SlashCommand[] {
    if (!input.startsWith("/")) return [];
    const query = input.slice(1).toLowerCase().trim();
    const byName = defaultSlashCommands.filter(c =>
      c.name.slice(1).toLowerCase().startsWith(query)
    );
    if (byName.length > 0) {
      if (this.selectedIdx >= byName.length) this.selectedIdx = 0;
      return byName;
    }
    const byDesc = defaultSlashCommands.filter(c =>
      c.desc.toLowerCase().includes(query)
    );
    if (this.selectedIdx >= byDesc.length) this.selectedIdx = 0;
    return byDesc;
  }

  render(filtered: SlashCommand[], width: number): string[] {
    if (filtered.length === 0) return [];
    const lines: string[] = [];
    const boxWidth = Math.max(40, width - 4);

    // Top title bar
    const title = ` ${symbols.brand} COMMAND PALETTE [↑↓: Navigate | Enter: Select | Esc: Cancel] `;
    const barLen = Math.max(0, boxWidth - visibleWidth(title) - 4);
    lines.push(
      theme.borderActive("╭─") +
      theme.bold(theme.accent(title)) +
      theme.borderActive("─".repeat(barLen) + "╮")
    );

    const maxItems = Math.min(filtered.length, 6);
    for (let i = 0; i < maxItems; i++) {
      const cmd = filtered[i];
      const isSel = i === this.selectedIdx;

      const cursor = isSel ? theme.bold(theme.orange(" ▶ ")) : "   ";
      const icon = `${cmd.icon} `;
      const name = isSel ? theme.bold(theme.white(cmd.name.padEnd(12))) : theme.accent(cmd.name.padEnd(12));
      const desc = isSel ? theme.white(cmd.desc) : theme.dim(cmd.desc);

      const content = `${cursor}${icon}${name} ${desc}`;
      const innerWidth = boxWidth - 2;

      let rowContent = "";
      if (isSel) {
        // High-contrast highlighted line
        rowContent = `\x1b[48;5;238m${padRight(content, innerWidth)}\x1b[0m`;
      } else {
        rowContent = padRight(content, innerWidth);
      }

      lines.push(theme.borderActive("│") + rowContent + theme.borderActive("│"));
    }

    lines.push(theme.borderActive("╰" + "─".repeat(boxWidth - 2) + "╯"));
    return lines;
  }
}
