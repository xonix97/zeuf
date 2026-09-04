import { describe, it, expect } from "bun:test";
import {
  initSessionMemory,
  addChatMemory,
  removeChatMemory,
  clearChatMemory,
  formatChatMemoryForPrompt,
  extractAndSaveAutoMemory,
} from "../src/core/memory";
import type { SessionData } from "../src/core/types";
import { ToolRegistry } from "../src/tools/registry";

function createMockSession(id: string = "test-session"): SessionData {
  return {
    id,
    task: "test task",
    createdAt: Date.now(),
    updatedAt: Date.now(),
    model: "auto",
    messages: [],
    modifiedFiles: [],
    checkpoints: [],
    memory: [],
  };
}

describe("Zeuf Per-Chat Session Memory Layer", () => {
  it("initializes and adds memory items to a chat session", () => {
    const session = createMockSession();
    expect(initSessionMemory(session)).toEqual([]);

    const res1 = addChatMemory(session, "User prefers concise answers");
    expect(res1.added).toBe(true);
    expect(res1.count).toBe(1);
    expect(session.memory).toContain("User prefers concise answers");

    // Deduplication check
    const res2 = addChatMemory(session, "user prefers concise answers");
    expect(res2.added).toBe(false);
    expect(res2.count).toBe(1);

    const res3 = addChatMemory(session, "Target platform is Linux");
    expect(res3.added).toBe(true);
    expect(res3.count).toBe(2);
    expect(session.memory?.length).toBe(2);
  });

  it("removes memory items from a chat session", () => {
    const session = createMockSession();
    addChatMemory(session, "Use TypeScript strict mode");
    addChatMemory(session, "Target ES2022");

    const removed = removeChatMemory(session, "strict mode");
    expect(removed).toBe(true);
    expect(session.memory).not.toContain("Use TypeScript strict mode");
    expect(session.memory).toContain("Target ES2022");

    const removeNonExistent = removeChatMemory(session, "nonexistent");
    expect(removeNonExistent).toBe(false);
  });

  it("clears chat memory properly", () => {
    const session = createMockSession();
    addChatMemory(session, "Fact 1");
    addChatMemory(session, "Fact 2");
    expect(session.memory?.length).toBe(2);

    clearChatMemory(session);
    expect(session.memory).toEqual([]);
  });

  it("formats chat memory context for system prompt", () => {
    const session = createMockSession();
    expect(formatChatMemoryForPrompt(session)).toBe("");

    addChatMemory(session, "Project uses Bun runtime");
    const promptContext = formatChatMemoryForPrompt(session);
    expect(promptContext).toContain("CHAT MEMORY");
    expect(promptContext).toContain("Project uses Bun runtime");
  });

  it("auto-extracts user introductions and preferences into chat session", () => {
    const session = createMockSession();

    const res1 = extractAndSaveAutoMemory(session, "hi my name is aurasobio");
    expect(res1.remembered).toBe(true);
    expect(res1.fact).toContain("aurasobio");
    expect(session.memory).toContain("User's name is aurasobio");

    const res2 = extractAndSaveAutoMemory(session, "remember that we deploy to Cloudflare");
    expect(res2.remembered).toBe(true);
    expect(res2.fact).toContain("we deploy to Cloudflare");
    expect(session.memory).toContain("we deploy to Cloudflare");
  });

  it("ensures strict session isolation: Chat A memory does not leak into Chat B", () => {
    const chatA = createMockSession("chat-a");
    const chatB = createMockSession("chat-b");

    addChatMemory(chatA, "Secret token for project A");
    expect(chatA.memory).toContain("Secret token for project A");
    expect(chatB.memory).toEqual([]);
    expect(formatChatMemoryForPrompt(chatB)).toBe("");
  });

  it("executes remember and forget tools via ToolRegistry on active session", async () => {
    const session = createMockSession();
    const registry = new ToolRegistry(process.cwd(), true, undefined, session);

    // Verify tool definitions
    const defs = registry.definitions();
    expect(defs.some(d => d.name === "remember")).toBe(true);
    expect(defs.some(d => d.name === "forget")).toBe(true);

    // Execute remember tool
    const rememberRes = await registry.execute(
      "call-1",
      "remember",
      JSON.stringify({
        fact: "All tests must pass before git push",
      })
    );
    expect(rememberRes.isError).toBe(false);
    expect(rememberRes.content).toContain("Saved to chat memory");
    expect(session.memory).toContain("All tests must pass before git push");

    // Execute forget tool
    const forgetRes = await registry.execute(
      "call-2",
      "forget",
      JSON.stringify({
        query: "All tests must pass",
      })
    );
    expect(forgetRes.isError).toBe(false);
    expect(forgetRes.content).toContain("Removed memory matching");
    expect(session.memory?.length).toBe(0);
  });
});
