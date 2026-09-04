import type { Provider } from "./types";
import type { ChatRequest, ChatResponse, ModelInfo, StreamEvent } from "../core/types";
import type { RoutingMode } from "../core/config";
import { OpenCodeProvider } from "./opencode";
import { AnthropicProvider } from "./anthropic";
import { DeepSeekProvider } from "./deepseek";
import { OpenAIProvider } from "./openai";

export class Router {
  providers: Map<string, Provider> = new Map();
  modelsCache: ModelInfo[] = [];
  lastRefresh: number = 0;
  failedModels: Map<string, number> = new Map(); // key -> cooldown until timestamp

  constructor() {
    this.register(new OpenCodeProvider());
    this.register(new AnthropicProvider());
    this.register(new DeepSeekProvider());

    // OpenAI-compatible environment discovery
    if (process.env.OPENAI_API_KEY) {
      this.register(new OpenAIProvider("openai", "https://api.openai.com/v1", process.env.OPENAI_API_KEY));
    }
    if (process.env.OPENROUTER_API_KEY) {
      this.register(new OpenAIProvider("openrouter", "https://openrouter.ai/api/v1", process.env.OPENROUTER_API_KEY));
    }
    if (process.env.GROQ_API_KEY) {
      this.register(new OpenAIProvider("groq", "https://api.groq.com/openai/v1", process.env.GROQ_API_KEY));
    }
  }

  register(provider: Provider): void {
    this.providers.set(provider.name, provider);
  }

  async allModels(forceRefresh = false): Promise<ModelInfo[]> {
    const now = Date.now();
    if (!forceRefresh && this.modelsCache.length > 0 && now - this.lastRefresh < 60000) {
      return this.modelsCache;
    }

    const all: ModelInfo[] = [];
    for (const provider of this.providers.values()) {
      try {
        const available = await provider.isAvailable();
        if (available) {
          const ms = await provider.listModels();
          all.push(...ms);
        }
      } catch {
        // Skip provider failure
      }
    }

    this.modelsCache = all;
    this.lastRefresh = now;
    return all;
  }

  rankModels(models: ModelInfo[], mode: RoutingMode = "auto", pinned?: string): ModelInfo[] {
    const now = Date.now();
    const available = models.filter(m => {
      const cooldown = this.failedModels.get(`${m.provider}/${m.id}`) || 0;
      return m.availability === "available" && now > cooldown;
    });

    if (pinned) {
      const found = available.find(m => m.id === pinned || `${m.provider}/${m.id}` === pinned);
      if (found) {
        return [found, ...available.filter(m => m !== found)];
      }
    }

    return available.sort((a, b) => {
      // Prioritize free models in auto/fastest mode
      if (mode === "auto" || mode === "fastest") {
        if (a.isFree && !b.isFree) return -1;
        if (!a.isFree && b.isFree) return 1;
      }
      const aScore = (a.scores?.coding || 70) + (a.scores?.reasoning || 70);
      const bScore = (b.scores?.coding || 70) + (b.scores?.reasoning || 70);
      return bScore - aScore;
    });
  }

  async executeWithFallback(
    req: ChatRequest,
    mode: RoutingMode,
    pinned: string | undefined,
    onEvent: (ev: StreamEvent) => void
  ): Promise<{ response: ChatResponse; model: string }> {
    const models = await this.allModels();
    const ranked = this.rankModels(models, mode, pinned);

    if (ranked.length === 0) {
      throw new Error("No AI model backends available. Check API keys or run `opencode auth`.");
    }

    let lastError: Error | null = null;
    let prevModel = "";

    for (let i = 0; i < Math.min(ranked.length, 3); i++) {
      const target = ranked[i];
      const modelKey = target.id.includes("/") ? target.id : `${target.provider}/${target.id}`;
      const provider = this.providers.get(target.provider);
      if (!provider) continue;

      if (i > 0) {
        onEvent({
          type: "switch",
          switchedFrom: prevModel,
          switchedTo: modelKey,
          switchReason: lastError?.message || "fallback",
        });
      }

      try {
        const clonedReq = { ...req, model: target.id };
        const resp = await provider.stream(clonedReq, onEvent);
        return { response: resp, model: modelKey };
      } catch (err: any) {
        lastError = err;
        prevModel = modelKey;
        // Park failed model for 60s
        this.failedModels.set(modelKey, Date.now() + 60000);
      }
    }

    throw lastError || new Error("All model backends failed");
  }
}
