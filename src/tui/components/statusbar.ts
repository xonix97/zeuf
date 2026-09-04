import { theme, symbols } from "../theme";
import { visibleWidth, truncate } from "../utils";

export interface StatusState {
  model: string;
  busy: boolean;
  turnStart?: number;
  tokensIn: number;
  tokensOut: number;
  branch?: string;
  workdir?: string;
}

export function renderStatusBar(state: StatusState, width: number): string {
  const brandPill = `\x1b[48;5;31m\x1b[38;5;255m\x1b[1m ${symbols.brand} ZEUF \x1b[0m`;

  const modelColor = state.model.includes("free") ? "121" : "153";
  const modelShort = state.model.replace(/^opencode\//, "");
  const modelPill = `\x1b[48;5;238m\x1b[38;5;${modelColor}m\x1b[1m ${symbols.robot} ${truncate(modelShort, 26)} \x1b[0m`;

  let statusPill = "";
  if (state.busy && state.turnStart) {
    const elapsed = ((Date.now() - state.turnStart) / 1000).toFixed(1);
    statusPill = `\x1b[48;5;58m\x1b[38;5;220m\x1b[1m ${symbols.dot} WORKING (${elapsed}s) \x1b[0m`;
  } else {
    statusPill = `\x1b[48;5;22m\x1b[38;5;120m\x1b[1m ${symbols.dot} READY \x1b[0m`;
  }

  const leftParts = `${brandPill} ${modelPill} ${statusPill}`;

  const branchStr = state.branch ? `${symbols.branch} ${state.branch} ` : "";
  const totalTokens = state.tokensIn + state.tokensOut;
  const tokenStr = totalTokens > 0 ? `${totalTokens} tok ` : "";
  const keyHints = `${theme.dim("^P")} models ${theme.dim("/")} help`;

  const rightParts = `${branchStr}${tokenStr}│ ${keyHints} `;

  const leftLen = visibleWidth(leftParts);
  const rightLen = visibleWidth(rightParts);

  const spacesCount = Math.max(1, width - leftLen - rightLen);
  const fullLine = `${leftParts}${" ".repeat(spacesCount)}${rightParts}`;

  // Fill line with dark statusline background
  return `\x1b[48;5;235m${fullLine}\x1b[0m`;
}
