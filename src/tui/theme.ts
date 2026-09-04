import pc from "picocolors";

export const theme = {
  accent: (s: string) => pc.cyan(s),
  orange: (s: string) => pc.yellow(s),
  dim: (s: string) => pc.gray(s),
  bold: (s: string) => pc.bold(s),
  white: (s: string) => pc.white(s),
  green: (s: string) => pc.green(s),
  red: (s: string) => pc.red(s),
  bgDark: (s: string) => `\x1b[48;5;234m${s}\x1b[49m`,
  border: (s: string) => pc.gray(s),
  badge: (text: string, col: (s: string) => string = pc.cyan) =>
    `\x1b[48;5;236m${col(pc.bold(` ${text} `))}\x1b[49m`,
};
