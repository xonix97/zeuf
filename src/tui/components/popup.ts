import { theme } from "../theme";

export interface SlashCommand {
  name: string;
  desc: string;
}

export const defaultSlashCommands: SlashCommand[] = [
  { name: "/models", desc: "List and switch available AI models" },
  { name: "/connect", desc: "Attach model backend (OpenRouter, Ollama, API key)" },
  { name: "/clear", desc: "Clear current conversation viewport" },
  { name: "/sessions", desc: "List and resume previous sessions" },
  { name: "/status", desc: "Inspect current workspace and git status" },
  { name: "/help", desc: "Show keyboard shortcuts and command reference" },
  { name: "/exit", desc: "Quit Zeuf" },
];

export class CommandPopup {
  selectedIdx: number = 0;

  filter(input: string): SlashCommand[] {
    if (!input.startsWith("/")) return [];
    const query = input.slice(1).toLowerCase();
    return defaultSlashCommands.filter(c => c.name.slice(1).toLowerCase().includes(query));
  }

  render(filtered: SlashCommand[], width: number): string[] {
    if (filtered.length === 0) return [];
    const lines: string[] = [];
    const boxWidth = Math.min(60, width - 4);

    lines.push(theme.border("┌─ COMMANDS " + "─".repeat(Math.max(0, boxWidth - 12)) + "┐"));
    for (let i = 0; i < Math.min(filtered.length, 6); i++) {
      const cmd = filtered[i];
      const isSel = i === this.selectedIdx;
      const cursor = isSel ? theme.orange("› ") : "  ";
      const name = isSel ? theme.bold(theme.white(cmd.name.padEnd(12))) : theme.dim(cmd.name.padEnd(12));
      const desc = theme.dim(cmd.desc.slice(0, boxWidth - 18));
      const row = `${cursor}${name} ${desc}`;
      const padded = row + " ".repeat(Math.max(0, boxWidth - 2 - (cursor.length + name.length + desc.length)));
      lines.push(theme.border("│ ") + row + theme.border(" │"));
    }
    lines.push(theme.border("└" + "─".repeat(boxWidth) + "┘"));
    return lines;
  }
}
