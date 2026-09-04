import type { Provider } from "./types";
import type { ChatRequest, ChatResponse, ModelInfo, StreamEvent, ToolCall } from "../core/types";

export class AnthropicProvider implements Provider {
  name = "anthropic";

  private get apiKey(): string {
    return process.env.ANTHROPIC_API_KEY || "";
  }

  async isAvailable(): Promise<boolean> {
    return Boolean(this.apiKey.trim());
  }

  async listModels(): Promise<ModelInfo[]> {
    const authed = await this.isAvailable();
    return [
      {
        id: "claude-3-7-sonnet-20250219",
        provider: "anthropic",
        displayName: "Claude 3.7 Sonnet",
        contextLength: 200000,
        maxOutput: 64000,
        supportsTools: true,
        supportsStreaming: true,
        isFree: false,
        availability: authed ? "available" : "auth_error",
        scores: { coding: 95, reasoning: 96 },
      },
      {
        id: "claude-3-5-sonnet-20241022",
        provider: "anthropic",
        displayName: "Claude 3.5 Sonnet",
        contextLength: 200000,
        maxOutput: 8192,
        supportsTools: true,
        supportsStreaming: true,
        isFree: false,
        availability: authed ? "available" : "auth_error",
        scores: { coding: 93, reasoning: 92 },
      },
      {
        id: "claude-3-5-haiku-20241022",
        provider: "anthropic",
        displayName: "Claude 3.5 Haiku",
        contextLength: 200000,
        maxOutput: 8192,
        supportsTools: true,
        supportsStreaming: true,
        isFree: false,
        availability: authed ? "available" : "auth_error",
        scores: { coding: 86, reasoning: 84 },
      },
    ];
  }

  async chat(req: ChatRequest): Promise<ChatResponse> {
    let fullContent = "";
    let reasoning = "";
    const toolCalls: ToolCall[] = [];

    await this.stream(req, ev => {
      if (ev.type === "token" && ev.text) {
        fullContent += ev.text;
      }
      if (ev.type === "reasoning" && ev.reasoning) {
        reasoning += ev.reasoning;
      }
    });

    return {
      content: fullContent,
      reasoning,
      toolCalls,
      usage: {
        inputTokens: 0,
        outputTokens: Math.round(fullContent.length / 4),
      },
    };
  }

  async stream(req: ChatRequest, onEvent: (ev: StreamEvent) => void): Promise<ChatResponse> {
    if (!this.apiKey) {
      throw new Error("ANTHROPIC_API_KEY environment variable is not set");
    }

    const { system, messages } = this.formatMessages(req);
    const tools = req.tools?.map(t => ({
      name: t.name,
      description: t.description,
      input_schema: t.parameters,
    }));

    const body: Record<string, any> = {
      model: req.model.replace(/^anthropic\//, ""),
      max_tokens: req.maxTokens || 4096,
      messages,
      stream: true,
    };
    if (system) {
      body.system = system;
    }
    if (tools && tools.length > 0) {
      body.tools = tools;
    }

    const res = await fetch("https://api.anthropic.com/v1/messages", {
      method: "POST",
      headers: {
        "x-api-key": this.apiKey,
        "anthropic-version": "2023-06-01",
        "content-type": "application/json",
      },
      body: JSON.stringify(body),
    });

    if (!res.ok) {
      const errText = await res.text();
      throw new Error(`Anthropic API error (${res.status}): ${errText}`);
    }

    const reader = res.body?.getReader();
    if (!reader) {
      throw new Error("No response body stream from Anthropic");
    }

    const decoder = new TextDecoder();
    let buffer = "";
    let fullText = "";
    let reasoning = "";
    const toolCalls: ToolCall[] = [];
    let currentTool: { id: string; name: string; argsJSON: string } | null = null;
    let inputTokens = 0;
    let outputTokens = 0;

    while (true) {
      const { done, value } = await reader.read();
      if (done) break;

      buffer += decoder.decode(value, { stream: true });
      const lines = buffer.split("\n");
      buffer = lines.pop() || "";

      for (const line of lines) {
        if (!line.startsWith("data: ")) continue;
        const dataStr = line.slice(6).trim();
        if (dataStr === "[DONE]") continue;

        try {
          const ev = JSON.parse(dataStr);
          if (ev.type === "message_start" && ev.message?.usage) {
            inputTokens = ev.message.usage.input_tokens || 0;
          }
          if (ev.type === "content_block_start") {
            if (ev.content_block?.type === "tool_use") {
              currentTool = {
                id: ev.content_block.id,
                name: ev.content_block.name,
                argsJSON: "",
              };
            }
          }
          if (ev.type === "content_block_delta") {
            const delta = ev.delta;
            if (delta.type === "text_delta") {
              fullText += delta.text;
              onEvent({ type: "token", text: delta.text, model: req.model });
            } else if (delta.type === "thinking_delta") {
              reasoning += delta.thinking;
              onEvent({ type: "reasoning", reasoning: delta.thinking, model: req.model });
            } else if (delta.type === "input_json_delta" && currentTool) {
              currentTool.argsJSON += delta.partial_json;
            }
          }
          if (ev.type === "content_block_stop" && currentTool) {
            toolCalls.push({
              id: currentTool.id,
              name: currentTool.name,
              arguments: currentTool.argsJSON,
            });
            currentTool = null;
          }
          if (ev.type === "message_delta" && ev.usage) {
            outputTokens = ev.usage.output_tokens || 0;
          }
        } catch {
          // Ignore partial chunk parse error
        }
      }
    }

    const usage = { inputTokens, outputTokens };
    onEvent({ type: "usage", usage, model: req.model });
    onEvent({ type: "done", model: req.model });

    return {
      content: fullText,
      reasoning,
      toolCalls,
      usage,
    };
  }

  private formatMessages(req: ChatRequest): { system?: string; messages: any[] } {
    let system: string | undefined;
    const messages: any[] = [];

    for (const m of req.messages) {
      if (m.role === "system") {
        system = (system ? system + "\n\n" : "") + m.content;
      } else if (m.role === "user") {
        messages.push({ role: "user", content: m.content });
      } else if (m.role === "assistant") {
        const content: any[] = [];
        if (m.content) {
          content.push({ type: "text", text: m.content });
        }
        if (m.toolCalls) {
          for (const tc of m.toolCalls) {
            let parsedArgs = {};
            try { parsedArgs = JSON.parse(tc.arguments); } catch {}
            content.push({
              type: "tool_use",
              id: tc.id,
              name: tc.name,
              input: parsedArgs,
            });
          }
        }
        messages.push({ role: "assistant", content });
      } else if (m.role === "tool") {
        messages.push({
          role: "user",
          content: [
            {
              type: "tool_result",
              tool_use_id: m.id || "tool-call",
              content: m.content,
            },
          ],
        });
      }
    }

    return { system, messages };
  }
}
