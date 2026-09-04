import { describe, it, expect, beforeEach, afterEach } from "bun:test";
import { readFile, writeFile, editFile } from "../src/tools/file";
import { executeBash } from "../src/tools/shell";
import { executeGlob, executeGrep } from "../src/tools/search";
import { ToolRegistry } from "../src/tools/registry";
import * as fs from "fs";
import * as path from "path";
import * as os from "os";

describe("Zeuf Tools Layer", () => {
  let testDir: string;

  beforeEach(() => {
    testDir = fs.mkdtempSync(path.join(os.tmpdir(), "zeuf-test-"));
  });

  afterEach(() => {
    fs.rmSync(testDir, { recursive: true, force: true });
  });

  it("writes and reads a file correctly", async () => {
    const filePath = "sub/dir/test.txt";
    const writeResult = writeFile(testDir, { path: filePath, content: "Hello from Zeuf Bun rewrite!\nLine 2" });
    expect(writeResult.isError).toBe(false);

    const readResult = readFile(testDir, { path: filePath });
    expect(readResult.isError).toBe(false);
    expect(readResult.content).toContain("Hello from Zeuf Bun rewrite!");
  });

  it("edits a file with unique occurrence validation", async () => {
    const filePath = "code.ts";
    writeFile(testDir, {
      path: filePath,
      content: "const greeting = 'hello';\nconst target = 'replace_me';\nconst end = true;",
    });

    const editResult = editFile(testDir, {
      path: filePath,
      old_string: "const target = 'replace_me';",
      new_string: "const target = 'completed';",
    });
    expect(editResult.isError).toBe(false);

    const verifyRead = readFile(testDir, { path: filePath });
    expect(verifyRead.content).toContain("const target = 'completed';");
    expect(verifyRead.content).not.toContain("replace_me");
  });

  it("fails editFile if target string is not found", async () => {
    const filePath = "missing.txt";
    writeFile(testDir, { path: filePath, content: "some existing text" });

    const editResult = editFile(testDir, {
      path: filePath,
      old_string: "nonexistent pattern",
      new_string: "replacement",
    });
    expect(editResult.isError).toBe(true);
    expect(editResult.content).toContain("not found");
  });

  it("executes bash commands cleanly", async () => {
    const result = await executeBash(testDir, { command: "echo 'zeuf standalone binary'" });
    expect(result.exitCode).toBe(0);
    expect(result.content.trim()).toBe("zeuf standalone binary");
  });

  it("searches files via glob and grep", async () => {
    const fileA = "feature-alpha.ts";
    const fileB = "feature-beta.ts";
    writeFile(testDir, { path: fileA, content: "export const alphaToken = 'SUPER_SECRET_123';" });
    writeFile(testDir, { path: fileB, content: "export const betaToken = 'OTHER_TOKEN';" });

    const globResult = await executeGlob(testDir, { pattern: "*.ts" });
    expect(globResult.isError).toBe(false);
    expect(globResult.content).toContain("feature-alpha.ts");
    expect(globResult.content).toContain("feature-beta.ts");

    const grepResult = await executeGrep(testDir, { pattern: "SUPER_SECRET_123" });
    expect(grepResult.isError).toBe(false);
    expect(grepResult.content).toContain("alphaToken");
  });

  it("registers and invokes tools via ToolRegistry with permission approval", async () => {
    const registry = new ToolRegistry(testDir, true); // autoApprove = true
    const defs = registry.definitions();
    expect(defs.length).toBeGreaterThanOrEqual(6);

    const names = defs.map((d) => d.name);
    expect(names).toContain("read");
    expect(names).toContain("write");
    expect(names).toContain("edit");
    expect(names).toContain("bash");

    // Execute read tool via registry
    const testFile = "registry_test.txt";
    writeFile(testDir, { path: testFile, content: "registry content" });

    const execResult = await registry.execute(
      "call-1",
      "read",
      JSON.stringify({ path: testFile })
    );

    expect(execResult.isError).toBe(false);
    expect(execResult.content).toContain("registry content");
  });
});
