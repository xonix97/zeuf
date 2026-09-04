import { theme } from "../theme";

export interface StatusState {
  model: string;
  busy: boolean;
  turnStart?: number;
  tokensIn: number;
  tokensOut: number;
  branch?: string;
}

export function renderStatusBar(state: StatusState, width: number): string {
  const dot = state.busy ? theme.orange("●") : theme.green("●");
  const agentBadge = theme.badge("AGENT", s => theme.bold(theme.white(s)));
  const modelBadge = theme.badge(state.model || "auto", theme.accent);
  
  let workingTimer = "";
  if (state.busy && state.turnStart) {
    const elapsed = ((Date.now() - state.turnStart) / 1000).toFixed(1);
    workingTimer = theme.orange(` Working (${elapsed}s)…`);
  } else {
    workingTimer = theme.dim(" Ready");
  }

  const gitStr = state.branch ? theme.dim(` ⎇ ${state.branch}`) : "";
  const tokenStr = theme.dim(` ${state.tokensIn + state.tokensOut} tokens`);

  const left = ` ${dot} ${agentBadge} ${modelBadge}${workingTimer}`;
  const right = `${gitStr} |${tokenStr} | ${theme.dim("Ctrl+P models  /help")} `;

  const spaces = Math.max(0, width - (left.replace(/\x1b\[[0-9;]*m/g, "").length + right.replace(/\x1b\[[0-9;]*m/g, "").length));
  return left + " ".repeat(spaces) + right;
}
