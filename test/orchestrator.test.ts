import { describe, it, expect } from "bun:test";
import { isConversational } from "../src/agent/orchestrator";
import { ConversationContext } from "../src/agent/context";

describe("Zeuf Agent Orchestrator", () => {
  it("detects conversational greetings for fast-path sub-200ms routing", () => {
    expect(isConversational("hi")).toBe(true);
    expect(isConversational("Hello!")).toBe(true);
    expect(isConversational("hey?")).toBe(true);
    expect(isConversational("who are you")).toBe(true);
    expect(isConversational("what can you do?")).toBe(true);
    expect(isConversational("ping")).toBe(true);
    expect(isConversational("whats my name")).toBe(true);
    expect(isConversational("what is my name?")).toBe(true);
    expect(isConversational("who am i")).toBe(true);
    expect(isConversational("hi my name is aurasobio")).toBe(true);
    expect(isConversational("my name is bob")).toBe(true);

    // Coding and complex tasks should NOT trigger conversational fast-path
    expect(isConversational("refactor the auth module")).toBe(false);
    expect(isConversational("build this binary and install it")).toBe(false);
    expect(isConversational("grep for TODO in src/")).toBe(false);
    expect(isConversational("write a test for router")).toBe(false);
  });

  it("manages conversation history and prompt context", () => {
    const ctx = new ConversationContext("You are Zeuf system prompt");
    ctx.addUser("Previous question");
    ctx.addAssistant("Previous answer");
    ctx.addUser("New task prompt");

    const messages = ctx.getMessages();
    expect(messages.length).toBe(4);
    expect(messages[0].role).toBe("system");
    expect(messages[0].content).toContain("Zeuf system prompt");
    expect(messages[1].role).toBe("user");
    expect(messages[1].content).toBe("Previous question");
    expect(messages[2].role).toBe("assistant");
    expect(messages[2].content).toBe("Previous answer");
    expect(messages[3].role).toBe("user");
    expect(messages[3].content).toBe("New task prompt");
  });
});
