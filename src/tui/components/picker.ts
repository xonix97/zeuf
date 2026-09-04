import { theme } from "../theme";
import type { ModelInfo } from "../../core/types";

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
    const q = this.filterText.toLowerCase();
    return models.filter(m => m.id.toLowerCase().includes(q) || m.displayName.toLowerCase().includes(q));
  }

  render(models: ModelInfo[], currentModel: string, width: number, height: number): string[] {
    const lines: string[] = [];
    const filtered = this.getFiltered(models);
    const boxW = Math.min(70, width - 6);
    const boxH = Math.min(15, height - 4);

    lines.push(theme.border("┌─ SELECT MODEL (Ctrl+P to dismiss) " + "─".repeat(Math.max(0, boxW - 36)) + "┐"));
    lines.push(theme.border("│ ") + theme.dim("Filter: ") + theme.bold(this.filterText || "(type to search...)") + theme.border(" │"));
    lines.push(theme.border("├" + "─".repeat(boxW) + "┤"));

    const visibleItems = filtered.slice(0, boxH - 4);
    for (let i = 0; i < visibleItems.length; i++) {
      const m = visibleItems[i];
      const isSel = i === this.selectedIdx;
      const isCurrent = m.id === currentModel || `${m.provider}/${m.id}` === currentModel;
      const cursor = isSel ? theme.orange("› ") : "  ";
      const badge = m.isFree ? theme.green("[free]") : theme.dim("[paid]");
      const mark = isCurrent ? theme.accent(" (active)") : "";
      const label = `${cursor}${m.displayName} ${theme.dim(`(${m.provider})`)} ${badge}${mark}`;
      lines.push(theme.border("│ ") + label);
    }

    if (visibleItems.length === 0) {
      lines.push(theme.border("│ ") + theme.dim("  No matching models found"));
    }

    lines.push(theme.border("└" + "─".repeat(boxW) + "┘"));
    return lines;
  }
}
