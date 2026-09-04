import type { Provider } from "./types";
import type { ChatRequest, ChatResponse, ModelInfo, StreamEvent, ToolCall } from "../core/types";

export class DeepSeekProvider implements Provider {
  name = "deepseek";

  private get apiKey(): string {
    return process.env.DEEPSEEK_API_KEY || "";
  }

  async isAvailable(): Promise<boolean> {
    return Boolean(this.apiKey.trim());
  }

  async listModels(): Promise<ModelInfo[]> {
    const authed = await this.isAvailable();
    return [
      {
        id: "deepseek-chat",
        provider: "deepseek",
        displayName: "DeepSeek V3",
        contextLength: 64000,
        supportsTools: true,
        supportsStreaming: true,
        isFree: false,
        availability: authed ? "available" : "auth_error",
        scores: { coding: 92, reasoning: 90 },
      },
      {
        id: "deepseek-reasoner",
        provider: "deepseek",
        displayName: "DeepSeek R1",
        contextLength: 64000,
        supportsTools: true,
        supportsStreaming: true,
        isFree: false,
        availability: authed ? "available" : "auth_error",
        scores: { coding: 94, reasoning: 96 },
      },
    ];
  }

  async chat(req: ChatRequest): Promise<ChatResponse> {
    let fullContent = "";
    let reasoning = "";
    await this.stream(req, ev => {
      if (ev.type === "token" && ev.text) fullContent += ev.text;
      if (ev.type === "reasoning" && ev.reasoning) reasoning += ev.reasoning;
    });
    return {
      content: fullContent,
      reasoning,
      usage: { inputTokens: 0, outputTokens: Math.round(fullContent.length / 4) },
    };
  }

  async stream(req: ChatRequest, onEvent: (ev: StreamEvent) => void): Promise<ChatResponse> {
    if (!this.apiKey) {
      throw new Error("DEEPSEEK_API_KEY environment variable is not set");
    }

    const model = req.model.replace(/^deepseek\//, "");
    const res = await fetch("https://api.deepseek.com/chat/completions", {
      method: "POST",
      headers: {
        Authorization: `Bearer ${this.apiKey}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        model,
        messages: req.messages,
        stream: true,
      }),
    });

    if (!res.ok) {
      throw new Error(`DeepSeek API error (${res.status}): ${await res.text()}`);
    }

    const reader = res.body?.getReader();
    if (!reader) throw new Error("No response body stream from DeepSeek");

    const decoder = new TextDecoder();
    let buffer = "";
    let fullText = "";
    let reasoning = "";

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
          const parsed = JSON.parse(dataStr);
          const delta = parsed.choices?.[0]?.delta;
          if (delta?.reasoning_content) {
            reasoning += delta.reasoning_content;
            onEvent({ type: "reasoning", reasoning: delta.reasoning_content, model: req.model });
          }
          if (delta?.content) {
            fullText += delta.content;
            onEvent({ type: "token", text: delta.content, model: req.model });
          }
        } catch {}
      }
    }

    onEvent({ type: "done", model: req.model });
    return {
      content: fullText,
      reasoning,
      usage: { inputTokens: 0, outputTokens: Math.round(fullText.length / 4) },
    };
  }
}
