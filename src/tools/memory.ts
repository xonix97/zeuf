import { addMemoryItem, removeMemoryItem, type MemoryScope } from "../core/memory";

export interface RememberArgs {
  fact: string;
  category?: string;
  scope?: MemoryScope;
}

export interface ForgetArgs {
  query: string;
  scope?: MemoryScope;
}

export function rememberTool(
  workdir: string,
  args: RememberArgs
): { content: string; isError: boolean } {
  const fact = (args.fact || "").trim();
  if (!fact) {
    return { content: "Error: 'fact' parameter is required for remember tool.", isError: true };
  }

  const category = (args.category || "Conventions").trim();
  const scope: MemoryScope = args.scope === "global" ? "global" : "project";

  const { added, count } = addMemoryItem(workdir, fact, category, scope);
  if (added) {
    return {
      content: `Saved to ${scope} memory [${category}]: "${fact}" (Total active memories: ${count})`,
      isError: false,
    };
  } else {
    return {
      content: `Memory already contains: "${fact}" in ${scope} scope.`,
      isError: false,
    };
  }
}

export function forgetTool(
  workdir: string,
  args: ForgetArgs
): { content: string; isError: boolean } {
  const query = (args.query || "").trim();
  if (!query) {
    return { content: "Error: 'query' parameter is required for forget tool.", isError: true };
  }

  const scope: MemoryScope = args.scope === "global" ? "global" : "project";
  const removed = removeMemoryItem(workdir, query, scope);

  if (removed) {
    return {
      content: `Removed memory matching "${query}" from ${scope} memory.`,
      isError: false,
    };
  } else {
    return {
      content: `No memory found matching "${query}" in ${scope} memory.`,
      isError: false,
    };
  }
}
