import { theme, symbols } from "../theme";
import type { ModelInfo } from "../../core/types";
import { visibleWidth, padRight, truncate } from "../utils";

export class ModelPicker {
  active: boolean = false;
  selectedIdx: number = 0;
  filterText: string = "";

  open(): void {
    this.active = true;
    this.selectedIdx = 0;
    this.filterText = "";
  }

  close(): void {
    this.active = false;
  }

  getFiltered(models: ModelInfo[]): ModelInfo[] {
    if (!this.filterText) return models;
    const q = this.filterText.toLowerCase().trim();
    return models.filter(m =>
      m.id.toLowerCase().includes(q) ||
      (m.displayName && m.displayName.toLowerCase().includes(q)) ||
      m.provider.toLowerCase().includes(q)
    );
  }

  render(models: ModelInfo[], currentModel: string, width: number, height: number): string[] {
    const lines: string[] = [];
    const filtered = this.getFiltered(models);
    const boxW = Math.min(74, Math.max(46, width - 4));
    const boxH = Math.min(14, Math.max(8, height - 6));
    const innerW = boxW - 2;

    // Header
    const title = ` ${symbols.robot} SWITCH AI MODEL [^P / Esc to Close] `;
    const barLen = Math.max(0, boxW - visibleWidth(title) - 4);
    lines.push(
      theme.borderActive("╭─") +
      theme.bold(theme.accent(title)) +
      theme.borderActive("─".repeat(barLen) + "╮")
    );

    // Search bar
    const searchLabel = ` 🔍 Filter: `;
    const query = this.filterText ? theme.bold(theme.white(this.filterText)) : theme.dim("Type to search models...");
    const searchContent = searchLabel + query;
    lines.push(theme.borderActive("│") + padRight(searchContent, innerW) + theme.borderActive("│"));
    lines.push(theme.borderActive("├" + "─".repeat(innerW) + "┤"));

    const maxItems = Math.max(1, boxH - 4);
    const visibleItems = filtered.slice(0, maxItems);

    for (let i = 0; i < visibleItems.length; i++) {
      const m = visibleItems[i];
      const isSel = i === this.selectedIdx;
      const isCurrent = m.id === currentModel || `${m.provider}/${m.id}` === currentModel;

      const cursor = isSel ? theme.bold(theme.orange(" ▶ ")) : "   ";
      const icon = m.isFree ? theme.green("●") : theme.dim("○");
      const nameStr = isSel ? theme.bold(theme.white(m.displayName)) : theme.white(m.displayName);
      const provStr = theme.dim(`(${m.provider})`);
      const freeBadge = m.isFree ? theme.badgeSuccess("FREE") : theme.dim("[paid]");
      const activePill = isCurrent ? theme.bold(theme.accent(" [ACTIVE]")) : "";

      const left = `${cursor}${icon} ${nameStr} ${provStr}`;
      const right = `${freeBadge}${activePill} `;

      const spaces = Math.max(1, innerW - visibleWidth(left) - visibleWidth(right));
      const rowRaw = `${left}${" ".repeat(spaces)}${right}`;

      let rowStyled = "";
      if (isSel) {
        rowStyled = `\x1b[48;5;238m${padRight(rowRaw, innerW)}\x1b[0m`;
      } else {
        rowStyled = padRight(rowRaw, innerW);
      }

      lines.push(theme.borderActive("│") + rowStyled + theme.borderActive("│"));
    }

    if (visibleItems.length === 0) {
      lines.push(theme.borderActive("│") + padRight(theme.dim("   No matching models found"), innerW) + theme.borderActive("│"));
    }

    lines.push(theme.borderActive("╰" + "─".repeat(innerW) + "╯"));
    return lines;
  }
}
