import { theme } from "../theme";

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
    const boxW = Math.min(68, width - 4);

    lines.push(theme.orange("┌─ TOOL APPROVAL REQUIRED " + "─".repeat(Math.max(0, boxW - 26)) + "┐"));
    lines.push(theme.orange("│ ") + theme.bold("Tool: ") + theme.white(this.current.toolName));
    
    // Preview arguments
    const argsSummary = this.current.argsJSON.slice(0, boxW - 10);
    lines.push(theme.orange("│ ") + theme.dim("Args: ") + theme.dim(argsSummary));
    lines.push(theme.orange("├" + "─".repeat(boxW) + "┤"));
    lines.push(
      theme.orange("│ ") +
      theme.bold("[y]") + " Allow once   " +
      theme.bold("[a]") + " Always allow   " +
      theme.bold("[n/Esc]") + " Deny"
    );
    lines.push(theme.orange("└" + "─".repeat(boxW) + "┘"));
    return lines;
  }
}
