import { existsSync, readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { join, dirname } from "node:path";
import { homedir } from "node:os";

export type MemoryScope = "project" | "global";

export interface MemoryEntry {
  category: string;
  items: string[];
}

export function getProjectMemoryPath(workdir: string = process.cwd()): string {
  return join(workdir, ".zeuf", "memory.md");
}

export function getGlobalMemoryPath(): string {
  return join(homedir(), ".zeuf", "memory.md");
}

export function loadMemoryFile(filePath: string): string {
  try {
    if (existsSync(filePath)) {
      return readFileSync(filePath, "utf-8").trim();
    }
  } catch {
    // Return empty on read errors
  }
  return "";
}

export function saveMemoryFile(filePath: string, content: string): void {
  try {
    const dir = dirname(filePath);
    if (!existsSync(dir)) {
      mkdirSync(dir, { recursive: true, mode: 0o755 });
    }
    writeFileSync(filePath, content.trim() + "\n", { mode: 0o644 });
  } catch (err) {
    console.error(`Failed to save memory file at ${filePath}:`, err);
  }
}

/**
 * Parse markdown headings (## Category) and bullet lists (- item)
 */
export function parseMemoryMarkdown(markdown: string): Map<string, string[]> {
  const map = new Map<string, string[]>();
  if (!markdown.trim()) return map;

  const lines = markdown.split("\n");
  let currentCategory = "General";

  for (const rawLine of lines) {
    const line = rawLine.trim();
    if (line.startsWith("# ")) {
      continue; // Document title
    } else if (line.startsWith("## ")) {
      currentCategory = line.slice(3).trim();
      if (!map.has(currentCategory)) {
        map.set(currentCategory, []);
      }
    } else if (line.startsWith("- ") || line.startsWith("* ")) {
      const item = line.slice(2).trim();
      if (item) {
        if (!map.has(currentCategory)) {
          map.set(currentCategory, []);
        }
        map.get(currentCategory)!.push(item);
      }
    }
  }

  return map;
}

/**
 * Render structured categories back to markdown
 */
export function renderMemoryMarkdown(categories: Map<string, string[]>, title: string = "Zeuf Persistent Memory"): string {
  const parts: string[] = [`# ${title}\n`];

  for (const [cat, items] of categories.entries()) {
    if (items.length === 0) continue;
    parts.push(`## ${cat}`);
    for (const item of items) {
      parts.push(`- ${item}`);
    }
    parts.push("");
  }

  return parts.join("\n").trim();
}

/**
 * Add a memory item to project or global scope under a category
 */
export function addMemoryItem(
  workdir: string,
  item: string,
  category: string = "Conventions",
  scope: MemoryScope = "project"
): { added: boolean; count: number } {
  const filePath = scope === "project" ? getProjectMemoryPath(workdir) : getGlobalMemoryPath();
  const raw = loadMemoryFile(filePath);
  const categories = parseMemoryMarkdown(raw);

  const normalizedItem = item.trim();
  if (!normalizedItem) return { added: false, count: 0 };

  const normCat = category.trim() || "Conventions";
  const existing = categories.get(normCat) || [];

  // Deduplication check
  if (existing.some(i => i.toLowerCase() === normalizedItem.toLowerCase())) {
    return { added: false, count: existing.length };
  }

  existing.push(normalizedItem);
  categories.set(normCat, existing);

  const title = scope === "project" ? "Zeuf Project Memory" : "Zeuf Global Memory";
  saveMemoryFile(filePath, renderMemoryMarkdown(categories, title));

  let totalCount = 0;
  for (const items of categories.values()) totalCount += items.length;

  return { added: true, count: totalCount };
}

/**
 * Remove a memory item matching a query string
 */
export function removeMemoryItem(
  workdir: string,
  query: string,
  scope: MemoryScope = "project"
): boolean {
  const filePath = scope === "project" ? getProjectMemoryPath(workdir) : getGlobalMemoryPath();
  const raw = loadMemoryFile(filePath);
  if (!raw) return false;

  const categories = parseMemoryMarkdown(raw);
  const lowerQuery = query.toLowerCase().trim();
  let removed = false;

  for (const [cat, items] of categories.entries()) {
    const filtered = items.filter(item => {
      const match = item.toLowerCase().includes(lowerQuery);
      if (match) removed = true;
      return !match;
    });
    if (filtered.length === 0) {
      categories.delete(cat);
    } else {
      categories.set(cat, filtered);
    }
  }

  if (removed) {
    const title = scope === "project" ? "Zeuf Project Memory" : "Zeuf Global Memory";
    saveMemoryFile(filePath, renderMemoryMarkdown(categories, title));
  }

  return removed;
}

/**
 * Clear memory
 */
export function clearMemory(workdir: string, scope: "project" | "global" | "all" = "all"): void {
  if (scope === "project" || scope === "all") {
    const p = getProjectMemoryPath(workdir);
    if (existsSync(p)) {
      saveMemoryFile(p, "");
    }
  }
  if (scope === "global" || scope === "all") {
    const g = getGlobalMemoryPath();
    if (existsSync(g)) {
      saveMemoryFile(g, "");
    }
  }
}

/**
 * Load both project and global memory
 */
export function loadAllMemory(workdir: string = process.cwd()): {
  project: string;
  global: string;
  combined: string;
  itemCount: number;
} {
  const projectRaw = loadMemoryFile(getProjectMemoryPath(workdir));
  const globalRaw = loadMemoryFile(getGlobalMemoryPath());

  const projCats = parseMemoryMarkdown(projectRaw);
  const globCats = parseMemoryMarkdown(globalRaw);

  let itemCount = 0;
  for (const items of projCats.values()) itemCount += items.length;
  for (const items of globCats.values()) itemCount += items.length;

  const sections: string[] = [];

  if (projectRaw) {
    sections.push(`[Project Memory (${getProjectMemoryPath(workdir)})]\n${projectRaw}`);
  }
  if (globalRaw) {
    sections.push(`[Global Memory (${getGlobalMemoryPath()})]\n${globalRaw}`);
  }

  return {
    project: projectRaw,
    global: globalRaw,
    combined: sections.join("\n\n").trim(),
    itemCount,
  };
}

/**
 * Format memory strictly for system prompt inclusion
 */
export function formatMemoryForPrompt(workdir: string = process.cwd()): string {
  const { combined } = loadAllMemory(workdir);
  if (!combined) return "";

  return `PERSISTENT MEMORY & PROJECT CONTEXT (Rules, conventions, and facts learned across sessions):\n${combined}`;
}

/**
 * Auto-detect user introductions, explicit remember instructions, and preferences,
 * and immediately persist them into memory.
 */
export function extractAndSaveAutoMemory(
  workdir: string,
  text: string
): { remembered: boolean; fact?: string; scope?: MemoryScope } {
  const trimmed = text.trim();

  // 1. "my name is <name>" / "call me <name>" / "i am <name>"
  const nameMatch = trimmed.match(
    /(?:(?:hi|hello|hey|yo)[, ]+)?(?:my name is|i am|i'm|call me)\s+([a-zA-Z0-9_\- ]+?)(?:[.!,]|$)/i
  );
  if (nameMatch && nameMatch[1]) {
    const rawName = nameMatch[1].trim();
    const blacklist = [
      "a developer",
      "an engineer",
      "here",
      "ready",
      "building",
      "coding",
      "testing",
      "working",
      "fine",
      "good",
    ];
    if (!blacklist.includes(rawName.toLowerCase()) && rawName.length > 1 && rawName.length < 35) {
      const fact = `User's name is ${rawName}`;
      addMemoryItem(workdir, fact, "User Preferences", "global");
      return { remembered: true, fact, scope: "global" };
    }
  }

  // 2. "remember that <X>" or "remember to <X>" or "remember <X>"
  const rememberMatch = trimmed.match(/^remember(?:\s+that|\s+to)?\s+([^.!?]+)/i);
  if (rememberMatch && rememberMatch[1]) {
    const fact = rememberMatch[1].trim();
    if (fact.length > 3) {
      addMemoryItem(workdir, fact, "User Preferences", "project");
      return { remembered: true, fact, scope: "project" };
    }
  }

  // 3. "always use <X>" or "i prefer <X>"
  const prefMatch = trimmed.match(/^(?:always use|i prefer|prefer)\s+([^.!?]+)/i);
  if (prefMatch && prefMatch[1]) {
    const fact = trimmed.replace(/[.!]+$/, "").trim();
    addMemoryItem(workdir, fact, "Conventions", "project");
    return { remembered: true, fact, scope: "project" };
  }

  return { remembered: false };
}

