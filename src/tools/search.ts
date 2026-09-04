import { executeBash } from "./shell";

export interface GlobArgs {
  pattern: string;
  dir?: string;
}

export interface GrepArgs {
  pattern: string;
  dir?: string;
  includes?: string;
}

export async function executeGlob(
  workdir: string,
  args: GlobArgs
): Promise<{ content: string; isError: boolean }> {
  const targetDir = args.dir || ".";
  const cmd = `find ${targetDir} -type f -name "${args.pattern}" -not -path "*/.*" -not -path "*/node_modules/*" | head -n 100`;
  const res = await executeBash(workdir, { command: cmd, timeout_ms: 10000 });
  return { content: res.content || "No files matched pattern", isError: false };
}

export async function executeGrep(
  workdir: string,
  args: GrepArgs
): Promise<{ content: string; isError: boolean }> {
  const targetDir = args.dir || ".";
  const incFlag = args.includes ? `--glob "${args.includes}"` : "";
  const cmd = `rg -n --no-heading --max-count 50 ${incFlag} "${args.pattern}" ${targetDir} 2>/dev/null || grep -rn --exclude-dir={node_modules,.git} "${args.pattern}" ${targetDir} | head -n 50`;
  const res = await executeBash(workdir, { command: cmd, timeout_ms: 10000 });
  return { content: res.content || "No matches found", isError: false };
}
