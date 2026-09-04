import type { Router } from "../providers/router";
import type { ToolRegistry } from "../tools/registry";
import type { SessionData, StreamEvent, Message } from "../core/types";
import { ConversationContext } from "./context";
import { gitStatus } from "../tools/git";
import { formatChatMemoryForPrompt, extractAndSaveAutoMemory } from "../core/memory";

export function isConversational(task: string): boolean {
  const lower = task.trim().toLowerCase().replace(/[!?. ]+$/, "");
  switch (lower) {
    case "hi":
    case "hello":
    case "hey":
    case "yo":
    case "sup":
    case "howdy":
    case "hiya":
    case "ping":
    case "who are you":
    case "what are you":
    case "what is zeuf":
    case "help":
    case "what can you do":
    case "memory":
    case "show memory":
    case "what do you remember":
    case "what do you know":
    case "whats my name":
    case "what is my name":
    case "who am i":
    case "who is this":
    case "tell me what you remember":
    case "do you remember me":
      return true;
    default:
      if (lower.startsWith("hi ") || lower.startsWith("hello ") || lower.startsWith("hey ")) {
        return true;
      }
      if (lower.startsWith("my name is ") || lower.startsWith("i am ") || lower.startsWith("call me ")) {
        return true;
      }
      return false;
  }
}

export class Orchestrator {
  router: Router;
  tools: ToolRegistry;

  constructor(router: Router, tools: ToolRegistry) {
    this.router = router;
    this.tools = tools;
  }

  async execute(
    task: string,
    session: SessionData,
    onEvent: (ev: StreamEvent) => void,
    pinnedModel?: string
  ): Promise<string> {
    const taskTrim = task.trim();

    // Bind current chat session to tools so remember/forget operate on this chat
    this.tools.setSession(session);

    // Auto-extract and persist memory strictly for THIS chat session
    extractAndSaveAutoMemory(session, taskTrim);
    const memoryPrompt = formatChatMemoryForPrompt(session);

    // 1. Conversational Fast-Path
    if (isConversational(taskTrim)) {
      onEvent({ type: "phase", phase: "conversational" });

      const convMessages: Message[] = [
        {
          role: "system" as const,
          content: `You are Zeuf, a fast, reliable, developer-focused autonomous coding agent.
Respond warmly, concisely, and helpfully.
${memoryPrompt ? `\n${memoryPrompt}\n` : ""}`,
        },
      ];

      for (const m of session.messages) {
        if (m.role !== "system") {
          convMessages.push(m);
        }
      }
      convMessages.push({ role: "user", content: taskTrim });

      const req = {
        model: pinnedModel || "auto",
        messages: convMessages,
      };

      const { response, model } = await this.router.executeWithFallback(
        req,
        "auto",
        pinnedModel,
        onEvent
      );

      session.model = model;
      session.messages.push({ role: "user", content: taskTrim });
      session.messages.push({ role: "assistant", content: response.content });
      return response.content;
    }

    // 2. Full Engineering Agent Loop
    onEvent({ type: "phase", phase: "discovery" });
    const { branch, dirty } = await gitStatus(this.tools.workdir);

    const systemPrompt = `You are Zeuf, a serious, high-velocity autonomous coding agent.
Working directory: ${this.tools.workdir}
Git branch: ${branch || "none"}
Pre-existing dirty files: ${dirty.join(", ") || "clean"}
${memoryPrompt ? `\n${memoryPrompt}\n` : ""}
OPERATING RULES:
1. Act surgically and directly. Use your tools to inspect, edit, test, and verify.
2. Only run relevant tests when code is modified. Do not guess non-existent test runners.
3. Be concise and truthful in your final summary. Do not output marketing fluff.
4. If you learn a key project convention, architecture rule, or user preference, preserve it using the 'remember' tool.`;

    const ctx = new ConversationContext(systemPrompt);
    for (const m of session.messages) {
      if (m.role !== "system") {
        ctx.messages.push(m);
      }
    }
    ctx.addUser(taskTrim);
    session.messages.push({ role: "user", content: taskTrim });

    const maxTurns = 15;
    let finalAnswer = "";

    for (let turn = 0; turn < maxTurns; turn++) {
      onEvent({ type: "phase", phase: `turn_${turn + 1}` });

      const req = {
        model: pinnedModel || "auto",
        messages: ctx.getMessages(),
        tools: this.tools.definitions(),
      };

      const { response, model } = await this.router.executeWithFallback(
        req,
        "auto",
        pinnedModel,
        onEvent
      );

      session.model = model;

      if (response.content) {
        finalAnswer = response.content;
      }

      // If no tool calls, task is complete
      if (!response.toolCalls || response.toolCalls.length === 0) {
        ctx.addAssistant(response.content);
        session.messages.push({ role: "assistant", content: response.content });
        break;
      }

      // Record assistant turn with tool calls
      ctx.addAssistant(response.content, response.toolCalls);
      session.messages.push({ role: "assistant", content: response.content, toolCalls: response.toolCalls });

      // Execute tool calls
      for (const tc of response.toolCalls) {
        onEvent({
          type: "tool_start",
          toolName: tc.name,
          toolArgs: tc.arguments,
          model,
        });

        const start = Date.now();
        const res = await this.tools.execute(tc.id, tc.name, tc.arguments);
        const durationMs = Date.now() - start;

        onEvent({
          type: "tool_end",
          toolName: tc.name,
          text: res.content,
          toolOk: !res.isError,
          durationMs,
          model,
        });

        ctx.addToolResult(tc.id, tc.name, res.content);
        session.messages.push({ role: "tool", id: tc.id, name: tc.name, content: res.content });
      }
    }

    onEvent({ type: "done" });
    return finalAnswer;
  }
}
