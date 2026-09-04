import { theme, symbols } from "../theme";
import { visibleWidth, padRight, truncate } from "../utils";

export interface ApprovalRequest {
  toolName: string;
  argsJSON: string;
  resolve: (decision: "allow" | "always" | "deny") => void;
}

export class ApprovalModal {
  current: ApprovalRequest | null = null;

  render(width: number): string[] {
    if (!this.current) return [];
    const lines: string[] = [];
    const boxW = Math.min(72, Math.max(46, width - 4));
    const innerW = boxW - 2;

    const title = ` ⚠️  TOOL EXECUTION PERMISSION REQUIRED `;
    const barLen = Math.max(0, boxW - visibleWidth(title) - 4);
    lines.push(
      theme.orange("╭─") +
      theme.bold(theme.orange(title)) +
      theme.orange("─".repeat(barLen) + "╮")
    );

    const toolLine = ` ${symbols.zap} Action: ` + theme.bold(theme.white(this.current.toolName));
    lines.push(theme.orange("│") + padRight(toolLine, innerW) + theme.orange("│"));

    let argsPreview = this.current.argsJSON;
    try {
      const parsed = JSON.parse(this.current.argsJSON);
      argsPreview = JSON.stringify(parsed, null, 2).replace(/\n/g, " ");
    } catch {}

    const argsLine = ` 📄 Details: ` + theme.dim(truncate(argsPreview, innerW - 14));
    lines.push(theme.orange("│") + padRight(argsLine, innerW) + theme.orange("│"));
    lines.push(theme.orange("├" + "─".repeat(innerW) + "┤"));

    const optionsLine = ` [y] Allow Once   [a] Always Allow   [n/Esc] Deny`;
    lines.push(theme.orange("│") + padRight(theme.bold(theme.white(optionsLine)), innerW) + theme.orange("│"));
    lines.push(theme.orange("╰" + "─".repeat(innerW) + "╯"));

    return lines;
  }
}
