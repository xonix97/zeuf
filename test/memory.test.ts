import { describe, it, expect, beforeEach, afterEach } from "bun:test";
import * as fs from "fs";
import * as path from "path";
import * as os from "os";
import {
  addMemoryItem,
  removeMemoryItem,
  loadMemoryFile,
  parseMemoryMarkdown,
  renderMemoryMarkdown,
  clearMemory,
  loadAllMemory,
  formatMemoryForPrompt,
  getProjectMemoryPath,
} from "../src/core/memory";
import { ToolRegistry } from "../src/tools/registry";

describe("Zeuf Persistent Memory Layer", () => {
  let tempWorkdir: string;

  beforeEach(() => {
    tempWorkdir = fs.mkdtempSync(path.join(os.tmpdir(), "zeuf-mem-test-"));
  });

  afterEach(() => {
    fs.rmSync(tempWorkdir, { recursive: true, force: true });
  });

  it("parses and renders markdown memory sections accurately", () => {
    const raw = `# Zeuf Memory\n\n## Conventions\n- Always run bun test\n- Use strict typescript\n\n## Architecture\n- TUI is built with React Ink\n`;
    const parsed = parseMemoryMarkdown(raw);

    expect(parsed.has("Conventions")).toBe(true);
    expect(parsed.get("Conventions")).toEqual(["Always run bun test", "Use strict typescript"]);
    expect(parsed.has("Architecture")).toBe(true);
    expect(parsed.get("Architecture")).toEqual(["TUI is built with React Ink"]);

    const rendered = renderMemoryMarkdown(parsed, "Test Memory");
    expect(rendered).toContain("# Test Memory");
    expect(rendered).toContain("## Conventions");
    expect(rendered).toContain("- Always run bun test");
    expect(rendered).toContain("## Architecture");
    expect(rendered).toContain("- TUI is built with React Ink");
  });

  it("adds and deduplicates memory items in project scope", () => {
    const res1 = addMemoryItem(tempWorkdir, "Prefer bun over npm", "Conventions", "project");
    expect(res1.added).toBe(true);
    expect(res1.count).toBe(1);

    const memPath = getProjectMemoryPath(tempWorkdir);
    expect(fs.existsSync(memPath)).toBe(true);
    const content = loadMemoryFile(memPath);
    expect(content).toContain("Prefer bun over npm");

    // Deduplication check
    const res2 = addMemoryItem(tempWorkdir, "prefer bun over npm", "Conventions", "project");
    expect(res2.added).toBe(false);
    expect(res2.count).toBe(1);

    // Add another item under Architecture
    const res3 = addMemoryItem(tempWorkdir, "System binary installs to ~/.local/bin/zeuf", "Architecture", "project");
    expect(res3.added).toBe(true);
    expect(res3.count).toBe(2);

    const updatedContent = loadMemoryFile(memPath);
    expect(updatedContent).toContain("## Architecture");
    expect(updatedContent).toContain("- System binary installs to ~/.local/bin/zeuf");
  });

  it("removes memory items accurately", () => {
    addMemoryItem(tempWorkdir, "Rule A to keep", "Conventions", "project");
    addMemoryItem(tempWorkdir, "Rule B to delete", "Conventions", "project");

    const removed = removeMemoryItem(tempWorkdir, "Rule B", "project");
    expect(removed).toBe(true);

    const content = loadMemoryFile(getProjectMemoryPath(tempWorkdir));
    expect(content).toContain("Rule A to keep");
    expect(content).not.toContain("Rule B to delete");

    const removeNonExistent = removeMemoryItem(tempWorkdir, "NonExistent", "project");
    expect(removeNonExistent).toBe(false);
  });

  it("formats memory context cleanly for system prompt", () => {
    addMemoryItem(tempWorkdir, "Editorial theme colors #efe9dd", "Design", "project");

    const promptContext = formatMemoryForPrompt(tempWorkdir);
    expect(promptContext).toContain("PERSISTENT MEMORY & PROJECT CONTEXT");
    expect(promptContext).toContain("Editorial theme colors #efe9dd");
  });

  it("executes remember and forget tools via ToolRegistry", async () => {
    const registry = new ToolRegistry(tempWorkdir, true);

    // Verify tool definitions include remember and forget
    const defs = registry.definitions();
    expect(defs.some(d => d.name === "remember")).toBe(true);
    expect(defs.some(d => d.name === "forget")).toBe(true);

    // Execute remember tool
    const rememberRes = await registry.execute(
      "call-1",
      "remember",
      JSON.stringify({
        fact: "All tests must pass before git push",
        category: "Workflows",
        scope: "project",
      })
    );
    expect(rememberRes.isError).toBe(false);
    expect(rememberRes.content).toContain("Saved to project memory");

    // Execute forget tool
    const forgetRes = await registry.execute(
      "call-2",
      "forget",
      JSON.stringify({
        query: "All tests must pass",
        scope: "project",
      })
    );
    expect(forgetRes.isError).toBe(false);
    expect(forgetRes.content).toContain("Removed memory matching");
  });

  it("clears project memory properly", () => {
    addMemoryItem(tempWorkdir, "Temporary memory", "Conventions", "project");
    expect(fs.existsSync(getProjectMemoryPath(tempWorkdir))).toBe(true);

    clearMemory(tempWorkdir, "project");
    const content = loadMemoryFile(getProjectMemoryPath(tempWorkdir));
    expect(content).toBe("");
  });
});
