import type { Provider } from "./types";
import type { ChatRequest, ChatResponse, ModelInfo, StreamEvent, ToolCall } from "../core/types";

export class OpenAIProvider implements Provider {
  name: string;
  baseUrl: string;
  apiKey: string;
  models: ModelInfo[];

  constructor(
    name: string = "openai",
    baseUrl: string = "https://api.openai.com/v1",
    apiKey: string = process.env.OPENAI_API_KEY || "",
    models: ModelInfo[] = []
  ) {
    this.name = name;
    this.baseUrl = baseUrl.replace(/\/$/, "");
    this.apiKey = apiKey;
    this.models = models;
  }

  async isAvailable(): Promise<boolean> {
    if (this.name === "ollama") return true;
    return Boolean(this.apiKey.trim());
  }

  async listModels(): Promise<ModelInfo[]> {
    if (this.models.length > 0) {
      return this.models;
    }
    const authed = await this.isAvailable();
    return [
      {
        id: "gpt-4o",
        provider: this.name,
        displayName: "GPT-4o",
        contextLength: 128000,
        supportsTools: true,
        supportsStreaming: true,
        isFree: false,
        availability: authed ? "available" : "auth_error",
        scores: { coding: 92, reasoning: 91 },
      },
      {
        id: "gpt-4o-mini",
        provider: this.name,
        displayName: "GPT-4o Mini",
        contextLength: 128000,
        supportsTools: true,
        supportsStreaming: true,
        isFree: false,
        availability: authed ? "available" : "auth_error",
        scores: { coding: 85, reasoning: 83 },
      },
    ];
  }

  async chat(req: ChatRequest): Promise<ChatResponse> {
    let fullContent = "";
    await this.stream(req, ev => {
      if (ev.type === "token" && ev.text) fullContent += ev.text;
    });
    return {
      content: fullContent,
      usage: { inputTokens: 0, outputTokens: Math.round(fullContent.length / 4) },
    };
  }

  async stream(req: ChatRequest, onEvent: (ev: StreamEvent) => void): Promise<ChatResponse> {
    const headers: Record<string, string> = { "Content-Type": "application/json" };
    if (this.apiKey) {
      headers.Authorization = `Bearer ${this.apiKey}`;
    }

    const model = req.model.replace(new RegExp(`^${this.name}/`), "");
    const res = await fetch(`${this.baseUrl}/chat/completions`, {
      method: "POST",
      headers,
      body: JSON.stringify({
        model,
        messages: req.messages,
        stream: true,
      }),
    });

    if (!res.ok) {
      throw new Error(`${this.name} API error (${res.status}): ${await res.text()}`);
    }

    const reader = res.body?.getReader();
    if (!reader) throw new Error("No response body stream");

    const decoder = new TextDecoder();
    let buffer = "";
    let fullText = "";

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
      usage: { inputTokens: 0, outputTokens: Math.round(fullText.length / 4) },
    };
  }
}
