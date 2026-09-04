import { executeBash } from "./shell";

export async function gitStatus(workdir: string): Promise<{ branch: string; dirty: string[] }> {
  const branchRes = await executeBash(workdir, { command: "git branch --show-current", timeout_ms: 5000 });
  const statusRes = await executeBash(workdir, { command: "git status --porcelain", timeout_ms: 5000 });

  const branch = branchRes.isError ? "" : branchRes.content.trim();
  const dirty: string[] = [];
  if (!statusRes.isError && statusRes.content) {
    for (const line of statusRes.content.split("\n")) {
      const trimmed = line.trim();
      if (trimmed) {
        const parts = trimmed.split(/\s+/);
        dirty.push(parts[1] || trimmed);
      }
    }
  }

  return { branch, dirty };
}

export async function gitDiff(workdir: string): Promise<{ diffStat: string; rawDiff: string }> {
  const statRes = await executeBash(workdir, { command: "git diff --stat", timeout_ms: 5000 });
  const diffRes = await executeBash(workdir, { command: "git diff", timeout_ms: 5000 });
  return {
    diffStat: statRes.isError ? "" : statRes.content.trim(),
    rawDiff: diffRes.isError ? "" : diffRes.content.trim(),
  };
}
