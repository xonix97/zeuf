import pc from "picocolors";

export const theme = {
  // Foreground Colors
  accent: (s: string) => pc.cyan(s),
  orange: (s: string) => pc.yellow(s),
  dim: (s: string) => pc.gray(s),
  bold: (s: string) => pc.bold(s),
  white: (s: string) => pc.white(s),
  green: (s: string) => pc.green(s),
  red: (s: string) => pc.red(s),
  blue: (s: string) => pc.blue(s),
  magenta: (s: string) => pc.magenta(s),

  // Background / Surface Styles
  bgBrand: (s: string) => `\x1b[48;5;31m\x1b[38;5;255m\x1b[1m ${s} \x1b[0m`,
  bgPill: (s: string, fgColor: (t: string) => string = pc.cyan) =>
    `\x1b[48;5;236m ${fgColor(pc.bold(s))} \x1b[0m`,
  bgCard: (s: string) => `\x1b[48;5;235m${s}\x1b[0m`,
  bgSelect: (s: string) => `\x1b[48;5;24m\x1b[38;5;255m\x1b[1m${s}\x1b[0m`,
  bgStatus: (s: string) => `\x1b[48;5;234m${s}\x1b[0m`,

  // Borders & Dividers
  border: (s: string) => pc.gray(s),
  borderActive: (s: string) => pc.cyan(s),
  borderMuted: (s: string) => `\x1b[38;5;238m${s}\x1b[0m`,

  // Badges
  badge: (text: string, col: (s: string) => string = pc.cyan) =>
    `\x1b[48;5;237m ${col(pc.bold(text))} \x1b[0m`,

  badgeSuccess: (text: string) => `\x1b[48;5;22m\x1b[38;5;120m\x1b[1m ${text} \x1b[0m`,
  badgeWarning: (text: string) => `\x1b[48;5;58m\x1b[38;5;220m\x1b[1m ${text} \x1b[0m`,
  badgeError: (text: string) => `\x1b[48;5;52m\x1b[38;5;203m\x1b[1m ${text} \x1b[0m`,
};

export const symbols = {
  brand: "◈",
  diamond: "◆",
  spark: "✦",
  dot: "●",
  hollow: "○",
  check: "✓",
  cross: "✗",
  arrow: "▶",
  branch: "⎇",
  folder: "📁",
  zap: "⚡",
  gear: "⚙",
  bulb: "💡",
  chat: "💬",
  user: "",
  bot: "🤖",
  robot: "󰚩",
  key: "⌨",
  spin: ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"],
};
