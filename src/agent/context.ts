import type { Message, ToolCall } from "../core/types";

export class ConversationContext {
  messages: Message[] = [];
  systemPrompt: string;

  constructor(systemPrompt: string) {
    this.systemPrompt = systemPrompt;
    this.messages.push({ role: "system", content: systemPrompt });
  }

  addUser(content: string): void {
    this.messages.push({ role: "user", content });
  }

  addAssistant(content: string, toolCalls?: ToolCall[]): void {
    this.messages.push({ role: "assistant", content, toolCalls });
  }

  addToolResult(toolCallId: string, name: string, content: string): void {
    this.messages.push({ role: "tool", id: toolCallId, name, content });
  }

  getMessages(): Message[] {
    return this.messages;
  }
}
